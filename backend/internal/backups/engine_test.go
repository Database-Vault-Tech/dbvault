package backups

import (
	"strings"
	"testing"
	"time"
)

func TestObjectKeyIsPredictable(t *testing.T) {
	ts := time.Date(2026, 9, 19, 12, 30, 0, 0, time.UTC)
	got := ObjectKey("", "production", ts, "zstd", false, "")
	if got != "production/2026/09/19/backup_2026-09-19_12-30-00.dump.zst" {
		t.Fatalf("got %q", got)
	}
	got = ObjectKey("team/", "Prod DB!", ts, "gzip", true, "ab12cd34")
	if got != "team/prod-db/2026/09/19/backup_2026-09-19_12-30-00_ab12cd34.dump.gz.age" {
		t.Fatalf("got %q", got)
	}
}

func TestSlugNameCannotTraverse(t *testing.T) {
	for _, in := range []string{"../../etc", "a/b", "..", "   "} {
		s := SlugName(in)
		if strings.Contains(s, "/") || strings.HasPrefix(s, ".") || s == "" {
			t.Errorf("SlugName(%q) = %q is unsafe", in, s)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 505413632: "482.0 MB", 197568495616: "184.0 GB"}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
