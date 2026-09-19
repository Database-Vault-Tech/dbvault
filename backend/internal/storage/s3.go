package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3 stores backups in any S3-compatible object store: Amazon S3,
// Cloudflare R2 and MinIO all speak the same API.
type S3 struct {
	client *s3.Client
	bucket string
	label  string
	// minPartSize is the first multipart part size; it grows for very large
	// uploads so the 10,000-part S3 limit is never reached.
	minPartSize int
}

const (
	defaultPartSize = 16 << 20
	maxParts        = 10000
)

// NewS3 builds an S3-compatible client for typ (s3, r2 or minio).
func NewS3(ctx context.Context, typ string, cfg Config, creds Credentials) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("bucket is required")
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return nil, errors.New("access key and secret key are required")
	}

	region := cfg.Region
	endpoint := cfg.Endpoint
	pathStyle := cfg.ForcePathStyle
	switch typ {
	case TypeR2:
		region = "auto"
		if endpoint == "" {
			if cfg.AccountID == "" {
				return nil, errors.New("account ID or endpoint is required for Cloudflare R2")
			}
			endpoint = "https://" + cfg.AccountID + ".r2.cloudflarestorage.com"
		}
	case TypeMinIO:
		if endpoint == "" {
			return nil, errors.New("endpoint is required for MinIO")
		}
		pathStyle = true
		if region == "" {
			region = "us-east-1"
		}
	case TypeS3:
		if region == "" {
			return nil, errors.New("region is required for Amazon S3")
		}
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, "")),
		// Only send/validate checksums when the API requires them: several
		// S3-compatible providers reject the newer default CRC headers.
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = pathStyle
	})

	label := "s3://" + cfg.Bucket
	if typ == TypeR2 {
		label = "r2://" + cfg.Bucket
	} else if typ == TypeMinIO {
		label = "minio://" + cfg.Bucket
	}
	return &S3{client: client, bucket: cfg.Bucket, label: label, minPartSize: defaultPartSize}, nil
}

func (s *S3) Describe() string { return s.label }

// partSizeFor returns the part size for 1-based part number n. Part sizes
// may differ within one upload (all but the last must be >= 5 MiB), so we
// start small and grow: 16 MiB parts cover the first 32 GiB, then 64 MiB,
// then 256 MiB, which comfortably exceeds 1 TiB within 10,000 parts.
func (s *S3) partSizeFor(n int) int {
	switch {
	case n <= 2000:
		return s.minPartSize
	case n <= 6000:
		return s.minPartSize * 4
	default:
		return s.minPartSize * 16
	}
}

func (s *S3) Upload(ctx context.Context, key string, r io.Reader) (int64, error) {
	// Read the first part; small objects are sent with a single PutObject.
	buf := make([]byte, s.partSizeFor(1))
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, err
	}
	if n < len(buf) {
		_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(key),
			Body:          bytes.NewReader(buf[:n]),
			ContentLength: aws.Int64(int64(n)),
			ContentType:   aws.String("application/octet-stream"),
		})
		if err != nil {
			return 0, wrapS3Err("upload", err)
		}
		return int64(n), nil
	}

	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return 0, wrapS3Err("start multipart upload", err)
	}
	uploadID := created.UploadId
	abort := func(cause error) (int64, error) {
		_, _ = s.client.AbortMultipartUpload(context.WithoutCancel(ctx), &s3.AbortMultipartUploadInput{
			Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: uploadID,
		})
		return 0, cause
	}

	var parts []s3types.CompletedPart
	var total int64
	part := 1
	chunk := buf[:n]
	for {
		out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(key),
			UploadId:      uploadID,
			PartNumber:    aws.Int32(int32(part)),
			Body:          bytes.NewReader(chunk),
			ContentLength: aws.Int64(int64(len(chunk))),
		})
		if err != nil {
			return abort(wrapS3Err(fmt.Sprintf("upload part %d", part), err))
		}
		parts = append(parts, s3types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(int32(part))})
		total += int64(len(chunk))

		part++
		if part > maxParts {
			return abort(errors.New("backup exceeds the maximum multipart upload size"))
		}
		if size := s.partSizeFor(part); size != len(buf) {
			buf = make([]byte, size)
		}
		n, err := io.ReadFull(r, buf)
		if n == 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
			break
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return abort(err)
		}
		chunk = buf[:n]
	}

	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(key),
		UploadId:        uploadID,
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return abort(wrapS3Err("complete multipart upload", err))
	}
	return total, nil
}

func (s *S3) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, wrapS3Err("download", err)
	}
	return out.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil && !isNotFound(err) {
		return wrapS3Err("delete", err)
	}
	return nil
}

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *S3) Stat(ctx context.Context, key string) (Object, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return Object{}, ErrNotFound
		}
		return Object{}, wrapS3Err("stat", err)
	}
	obj := Object{Key: key, Size: aws.ToInt64(out.ContentLength)}
	if out.LastModified != nil {
		obj.LastModified = *out.LastModified
	}
	return obj, nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, wrapS3Err("list", err)
		}
		for _, o := range page.Contents {
			obj := Object{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size)}
			if o.LastModified != nil {
				obj.LastModified = *o.LastModified
			}
			out = append(out, obj)
		}
	}
	return out, nil
}

func isNotFound(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var re interface{ HTTPStatusCode() int }
	return errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound
}

// wrapS3Err turns SDK errors into concise messages safe to show users
// (they never contain credentials).
func wrapS3Err(op string, err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.ErrorMessage()
		if msg == "" {
			msg = apiErr.ErrorCode()
		}
		return fmt.Errorf("%s: %s (%s)", op, msg, apiErr.ErrorCode())
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && strings.Contains(s, "dial tcp") {
		return fmt.Errorf("%s: cannot reach storage endpoint: %s", op, s[i+2:])
	}
	return fmt.Errorf("%s: %w", op, err)
}
