package main

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"

	"github.com/spf13/cobra"
)

// gdriveEndpoints is Google's OAuth, Drive and Sheets; a test puts stand-ins in
// their place.
var gdriveEndpoints = gdrive.GoogleEndpoints

// gdriveDir is where the sign-in is kept, ~/.config/mw/gdrive, never in the
// vault or anywhere that syncs.
var gdriveDir = filepath.Join(".config", "mw", "gdrive")

// permissionsLink is where he takes mw's access back at Google.
const permissionsLink = "https://myaccount.google.com/permissions"

func gdriveFiles() (gdrive.Files, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return gdrive.Files{}, fmt.Errorf("there is no home directory to keep the Drive sign-in in: %w", err)
	}
	return gdrive.Files{Dir: filepath.Join(home, gdriveDir)}, nil
}

// gdriveSession is the signed-in session, or an error saying to run
// mw gdrive auth. The client is the copy beside the token, else the shared
// Desktop client file.
func gdriveSession() (gdrive.Session, error) {
	files, err := gdriveFiles()
	if err != nil {
		return gdrive.Session{}, err
	}
	if _, err := files.LoadToken(); err != nil {
		return gdrive.Session{}, err
	}
	client, err := files.LoadClient()
	if errors.Is(err, os.ErrNotExist) {
		shared, cerr := config.GoogleClientFile()
		if cerr != nil {
			return gdrive.Session{}, cerr
		}
		client, err = gdrive.ReadClientFile(shared)
	}
	if err != nil {
		return gdrive.Session{}, fmt.Errorf("%w; run mw gdrive auth with --client-id and --client-secret", err)
	}
	return gdrive.Session{Endpoints: gdriveEndpoints, Files: files, Client: client}, nil
}

// newGdriveCmd builds `mw gdrive`: his Google Drive and Sheets, read and
// written signed in as him. Nothing is mirrored; a file is fetched only when a
// command names it. The token and client are kept in ~/.config/mw/gdrive/ (dir
// 0700, files 0600) and are never printed or logged.
func newGdriveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gdrive",
		Short: "Read and write his Google Drive and Sheets, signed in as him (auth, find, read, write, revoke)",
		Long: "gdrive reads and writes his Google Drive and Sheets, signed in as him through his own Desktop\n" +
			"OAuth client (scopes " + gdrive.DriveScope + ",\n" + gdrive.SheetsScope + " and " + gdrive.GmailScope + ", the last for mw gmail).\n" +
			"Nothing is mirrored: a file is fetched only when a command names it. The client and the\n" +
			"refresh token are kept in ~/.config/mw/gdrive/ (the directory mode 700, the files 600) and\n" +
			"are never printed or logged.",
	}
	cmd.AddCommand(newGdriveAuthCmd(), newGdriveFindCmd(), newGdriveReadCmd(), newGdriveWriteCmd(), newGdriveRevokeCmd())
	return cmd
}

func newGdriveAuthCmd() *cobra.Command {
	var id, secret string
	cmd := &cobra.Command{
		Use:   "auth [--client-id ID --client-secret S]",
		Short: "Sign in: print the consent URL, read the pasted-back http://localhost address, keep the token",
		Long: "auth prints a consent URL. Open it in any browser, sign in and allow access; the browser then\n" +
			"fails to load a http://localhost/?... page. Copy that whole address from the address bar and\n" +
			"paste it here. mw trades the code in it for a token and keeps the client and the token in\n" +
			"~/.config/mw/gdrive/ (directory mode 700, files 600).\n\n" +
			"The client is read from ~/.config/mw/google-client.json (Google's Desktop client JSON; the same\n" +
			"file mw google uses) when it is there. When it is not, --client-id and --client-secret are\n" +
			"written to that file (mode 600) and used.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			files, err := gdriveFiles()
			if err != nil {
				return err
			}
			shared, err := config.GoogleClientFile()
			if err != nil {
				return err
			}
			var client gdrive.Client
			writeShared := false
			if _, statErr := os.Stat(shared); statErr == nil {
				client, err = gdrive.ReadClientFile(shared)
				if err != nil {
					return err
				}
				if id != "" || secret != "" {
					fmt.Fprintf(out, "Using the client in %s; --client-id and --client-secret are ignored while it is there.\n", shared)
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			} else {
				if id == "" {
					return fmt.Errorf("there is no Google OAuth client at %s: pass --client-id and --client-secret from a Desktop app client made in the Google Cloud console, or save its JSON there", shared)
				}
				client = gdrive.Client{ID: id, Secret: secret}
				writeShared = true
			}

			account := gdrive.Account{Endpoints: gdriveEndpoints}
			flow, err := account.Begin(client)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Open this address in a browser, sign in and allow access:\n\n%s\n\n", flow.URL)
			fmt.Fprintf(out, "The browser will then fail to load a %s/?... page. Copy that whole address from the address bar and paste it here, then press Enter:\n", gdrive.Redirect)
			pasted, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil && !(errors.Is(err, io.EOF) && strings.TrimSpace(pasted) != "") {
				return fmt.Errorf("no address was pasted back: %w", err)
			}
			token, err := account.Finish(cmd.Context(), client, flow, pasted)
			if err != nil {
				return err
			}
			if writeShared {
				if err := gdrive.WriteClientFile(shared, client); err != nil {
					return err
				}
			}
			if err := files.SaveClient(client); err != nil {
				return err
			}
			if err := files.SaveToken(token); err != nil {
				return err
			}
			fmt.Fprintf(out, "Signed in. The client and token are kept in %s (mode 600).\n", files.Dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "client-id", "", "the Desktop client's ID, written to google-client.json when that file is not there")
	cmd.Flags().StringVar(&secret, "client-secret", "", "the Desktop client's secret, written with the ID")
	return cmd
}

func newGdriveFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find <name>",
		Short: "List Drive files whose name contains <name>, newest modified first",
		Long: "find lists the Drive files whose name contains <name>, newest modified first, one per line:\n" +
			"id, name, modified, mime type, tab-separated. It lists metadata only; nothing is downloaded.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := gdriveSession()
			if err != nil {
				return err
			}
			found, err := s.Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(found) == 0 {
				fmt.Fprintf(out, "no file named %q\n", args[0])
				return nil
			}
			for _, f := range found {
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", f.ID, oneLine(f.Name), f.Modified, f.MimeType)
			}
			return nil
		},
	}
}

func newGdriveReadCmd() *cobra.Command {
	var sheet, rng string
	cmd := &cobra.Command{
		Use:   "read <file-id> [--sheet T] [--range A1:Z99]",
		Short: "Print a spreadsheet's cells as CSV; with no --sheet, list its tabs",
		Long: "read prints the cells of tab --sheet (limited to --range, in A1 notation, when given) as CSV.\n" +
			"With neither --sheet nor --range it lists the spreadsheet's tabs instead; with --range alone\n" +
			"it reads that range of the first tab.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := gdriveSession()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if sheet == "" && rng == "" {
				title, tabs, err := s.Tabs(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", oneLine(title))
				for _, tab := range tabs {
					fmt.Fprintf(out, "  %s\t(%d rows, %d columns)\n", oneLine(tab.Title), tab.Rows, tab.Columns)
				}
				return nil
			}
			rows, err := s.Read(cmd.Context(), args[0], sheet, rng)
			if err != nil {
				return err
			}
			w := csv.NewWriter(out)
			if err := w.WriteAll(rows); err != nil {
				return err
			}
			return w.Error()
		},
	}
	cmd.Flags().StringVar(&sheet, "sheet", "", "the tab to read")
	cmd.Flags().StringVar(&rng, "range", "", "the cells to read, like A1:Z99")
	return cmd
}

func newGdriveWriteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "write <file-id> <range> <value>...",
		Short: "Update cells in place (one row, as if typed); a Budget file needs --yes",
		Long: "write puts the values in one row starting at <range> (like Sheet1!A1:C1), updating the cells\n" +
			"in place as if he had typed them (a leading = makes a formula). It prints the range and cell\n" +
			"count Google changed, what the cells held before and what they hold now.\n\n" +
			"A file whose name contains 'Budget' is refused unless --yes is given, a guard against a stray\n" +
			"write to his budget; nothing is changed when it refuses.",
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := gdriveSession()
			if err != nil {
				return err
			}
			done, err := s.Write(cmd.Context(), args[0], args[1], args[2:], yes)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "updated %s (%d cells) in %q\n", done.Range, done.Cells, oneLine(done.Title))
			fmt.Fprintf(out, "was: %s\n", csvLine(done.Before))
			fmt.Fprintf(out, "now: %s\n", csvLine(done.After))
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "write to a file whose name contains 'Budget'")
	return cmd
}

func newGdriveRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Delete the kept client and token, and say where to take the access back at Google",
		Long: "revoke deletes the client copy and the token from ~/.config/mw/gdrive/ and prints the link where\n" +
			"he removes mw's access at Google. The shared google-client.json is left as it is.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			files, err := gdriveFiles()
			if err != nil {
				return err
			}
			gone, err := files.Remove()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(gone) == 0 {
				fmt.Fprintln(out, "nothing was kept here to delete")
			}
			for _, path := range gone {
				fmt.Fprintf(out, "deleted %s\n", path)
			}
			fmt.Fprintf(out, "To take mw's access away at Google too, remove it at %s\n", permissionsLink)
			return nil
		},
	}
}

// csvLine is rows as CSV on one line, rows joined by " / ", "(empty)" for none.
func csvLine(rows [][]string) string {
	if len(rows) == 0 {
		return "(empty)"
	}
	var parts []string
	for _, row := range rows {
		var b strings.Builder
		w := csv.NewWriter(&b)
		_ = w.Write(row)
		w.Flush()
		parts = append(parts, strings.TrimRight(b.String(), "\n"))
	}
	return strings.Join(parts, " / ")
}
