/*
Copyright © 2026 Pratyay360

*/
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

// licenseCmd prints the SPDX license text for a given license id.
var licenseCmd = &cobra.Command{
	Use:   "license ID",
	Short: "Print the SPDX text of a license",
	Long: `Print the SPDX text of a license.

The text is fetched from the SPDX license list, so it always reflects the
upstream wording. The copyright year and holder template fields are filled
in from --year and --holder.`,
	Args: cobra.ExactArgs(1),
	RunE: runLicense,
}

// licenseSearchCmd fuzzy-searches the SPDX license list and prints matches
// from best-ranked to worst. With no term it lists every known id.
var licenseSearchCmd = &cobra.Command{
	Use:   "search [term]",
	Short: "Fuzzy-search SPDX license identifiers",
	Long: `Fuzzy-search SPDX license identifiers and print them from the best
ranked match to the worst. With no term, every known identifier is listed
in alphabetical order.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLicenseSearch,
}

func init() {
	rootCmd.AddCommand(licenseCmd)
	licenseCmd.AddCommand(licenseSearchCmd)

	licenseCmd.Flags().String("year", "", "copyright year (default: current year)")
	licenseCmd.Flags().String("holder", "", "copyright holder")
	licenseCmd.Flags().StringP("output", "o", "", "write to a file instead of stdout")

	licenseSearchCmd.Flags().IntP("limit", "n", 10, "maximum number of matches to print")
}

func runLicense(cmd *cobra.Command, args []string) error {
	ctx, cancel := withTimeout(cmd)
	defer cancel()

	text, err := operations.FetchLicense(ctx, args[0])
	if err != nil {
		return err
	}

	year := flagString(cmd, "year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	holder := flagString(cmd, "holder")
	if holder == "" {
		holder = "[copyright holder]"
	}
	text = operations.ApplyLicenseTemplate(text, year, holder)

	if output := flagString(cmd, "output"); output != "" {
		if err := os.WriteFile(output, []byte(text), 0644); err != nil {
			return fmt.Errorf("writing license to %s: %w", output, err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", output)
		return nil
	}

	_, err = cmd.OutOrStdout().Write([]byte(text))
	return err
}

func runLicenseSearch(cmd *cobra.Command, args []string) error {
	ctx, cancel := withTimeout(cmd)
	defer cancel()

	term := ""
	if len(args) == 1 {
		term = args[0]
	}

	matches, err := operations.SearchLicenses(ctx, term)
	if err != nil {
		return err
	}

	limit := flagInt(cmd, "limit")
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	out := cmd.OutOrStdout()
	for _, m := range matches {
		if _, err := fmt.Fprintln(out, m.ID); err != nil {
			return err
		}
	}
	return nil
}
