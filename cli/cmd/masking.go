package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dbvault/dbvault/cli/internal/ui"
)

var maskingProfileName string

var maskingCmd = &cobra.Command{
	Use:   "masking",
	Short: "Manage masking profiles for anonymized restores",
	Long: `Masking profiles say how to anonymize a database when restoring it into
staging or development: which columns get fake values and which tables are
emptied. Profiles are YAML, so you can keep them in git:

  tables:
    users:
      email: email
      full_name: name
      phone: phone
      password_hash: { redact: masked }
    sessions: truncate

Then restore with: dbvault restore <backup-id> --new-database staging --mask default`,
}

// editorView is the part of GET /databases/{id}/masking the CLI uses.
type editorView struct {
	Supported         bool                `json:"supported"`
	UnsupportedReason string              `json:"unsupported_reason"`
	Schema            json.RawMessage     `json:"schema"`
	Suggested         json.RawMessage     `json:"suggested"`
	Profiles          []maskingProfile    `json:"profiles"`
	Problems          map[string][]string `json:"problems"`
}

type maskingProfile struct {
	Name    string          `json:"name"`
	Version int             `json:"version"`
	Rules   json.RawMessage `json:"rules"`
}

// printRules writes rules (JSON from the API) as YAML.
func printRules(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	out, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Print(string(out))
	return nil
}

func loadEditor(cmd *cobra.Command, name string) (string, editorView, error) {
	var ed editorView
	c, _, err := session()
	if err != nil {
		return "", ed, err
	}
	db, err := findDatabase(cmd.Context(), c, name)
	if err != nil {
		return "", ed, err
	}
	if err := c.Get(cmd.Context(), "/databases/"+db.ID+"/masking", &ed); err != nil {
		return "", ed, err
	}
	if !ed.Supported {
		return "", ed, errors.New(ed.UnsupportedReason)
	}
	return db.ID, ed, nil
}

var maskingSuggestCmd = &cobra.Command{
	Use:   "suggest <database>",
	Short: "Print suggested masking rules as YAML",
	Long: `Print rules DBVault suggests for the columns that look personal, based on the
schema recorded the last time a backup of this database was restore-tested.
Review them, then save them with "dbvault masking apply".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, ed, err := loadEditor(cmd, args[0])
		if err != nil {
			return err
		}
		if len(ed.Suggested) == 0 || string(ed.Suggested) == "null" {
			return fmt.Errorf("DBVault doesn't know %s's schema yet: verify one of its backups first (dbvault backup verify <backup-id>)", args[0])
		}
		if flagJSON {
			return printJSON(ed.Suggested)
		}
		return printRules(ed.Suggested)
	},
}

var maskingShowCmd = &cobra.Command{
	Use:   "show <database>",
	Short: "Print a saved masking profile as YAML",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, ed, err := loadEditor(cmd, args[0])
		if err != nil {
			return err
		}
		for _, p := range ed.Profiles {
			if p.Name != maskingProfileName {
				continue
			}
			if flagJSON {
				return printJSON(p)
			}
			fmt.Printf("# profile %q, version %d\n", p.Name, p.Version)
			if err := printRules(p.Rules); err != nil {
				return err
			}
			for _, msg := range ed.Problems[p.Name] {
				fmt.Fprintln(os.Stderr, ui.Yellow("! ")+msg)
			}
			return nil
		}
		return fmt.Errorf("%s has no masking profile named %q", args[0], maskingProfileName)
	},
}

var maskingFile string

var maskingApplyCmd = &cobra.Command{
	Use:   "apply <database> -f rules.yaml",
	Short: "Save masking rules from a YAML (or JSON) file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if maskingFile == "" {
			return errors.New("pass the rules file with -f")
		}
		data, err := os.ReadFile(maskingFile)
		if err != nil {
			return err
		}
		var rules any
		if err := yaml.Unmarshal(data, &rules); err != nil {
			return fmt.Errorf("%s: %w", maskingFile, err)
		}
		dbID, _, err := loadEditor(cmd, args[0])
		if err != nil {
			return err
		}
		c, _, err := session()
		if err != nil {
			return err
		}
		var res struct {
			Profile  maskingProfile `json:"profile"`
			Problems []string       `json:"problems"`
		}
		if err := c.Put(cmd.Context(), "/databases/"+dbID+"/masking/profiles/"+maskingProfileName, map[string]any{"rules": rules}, &res); err != nil {
			return err
		}
		if flagJSON {
			return printJSON(res)
		}
		fmt.Printf("%s Saved profile %q (version %d)\n", ui.Green("✓"), res.Profile.Name, res.Profile.Version)
		if len(res.Problems) > 0 {
			fmt.Println(ui.Yellow("\nA masked restore would stop until these are fixed:"))
			for _, p := range res.Problems {
				fmt.Println("  • " + p)
			}
		}
		return nil
	},
}

func init() {
	for _, c := range []*cobra.Command{maskingShowCmd, maskingApplyCmd} {
		c.Flags().StringVar(&maskingProfileName, "profile", "default", "profile name")
	}
	maskingApplyCmd.Flags().StringVarP(&maskingFile, "file", "f", "", "rules file (YAML or JSON)")
	maskingCmd.AddCommand(maskingSuggestCmd, maskingShowCmd, maskingApplyCmd)
	rootCmd.AddCommand(maskingCmd)
}
