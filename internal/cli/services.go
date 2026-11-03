package cli

import (
	"fmt"
	"strings"

	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/spf13/cobra"
)

var (
	servicesConsumersOf string
	servicesCallsFrom   string
	servicesDepth       int
	servicesSites       int
)

var queryServicesCmd = &cobra.Command{
	Use:   "services (--consumers-of <member> | --calls-from <member>)",
	Short: "Which workspace members call a member over HTTP, or which it calls",
	Long: `services lists HTTP dependencies between workspace members, from client
calls linked to the routes they reach and from reads of a member's declared
base-URL variables (synapse.yaml http.base_env).

--consumers-of lists the members that call one member; --depth 2 also lists
members that reach it through those callers. --calls-from lists the members a
member calls. --sites prints up to that many call sites per member.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if workspacePath == "" {
			return fmt.Errorf("query services needs --workspace")
		}
		if (servicesConsumersOf == "") == (servicesCallsFrom == "") {
			return fmt.Errorf("pass exactly one of --consumers-of or --calls-from")
		}
		opened, err := openWorkspaceStore(workspacePath, dataDir, "")
		if err != nil {
			return err
		}
		defer opened.Closer.Close()
		var res rank.ServiceResult
		if servicesConsumersOf != "" {
			member, err := opened.Workspace.Lookup(servicesConsumersOf)
			if err != nil {
				return err
			}
			res, err = rank.ServiceConsumers(opened.Store, member.Name, servicesDepth)
			if err != nil {
				return err
			}
		} else {
			member, err := opened.Workspace.Lookup(servicesCallsFrom)
			if err != nil {
				return err
			}
			res, err = rank.ServiceCalls(opened.Store, member.Name)
			if err != nil {
				return err
			}
		}
		if queryJSON {
			return writeJSON(cmd, res)
		}
		out := cmd.OutOrStdout()
		verb := "called by"
		if res.Direction == "calls" {
			verb = "calls"
		}
		fmt.Fprintf(out, "%s %s %d member(s)\n", res.Member, verb, len(res.Services))
		for _, l := range res.Services {
			var parts []string
			parts = append(parts, fmt.Sprintf("calls=%d", l.Calls))
			if len(l.EnvReads) > 0 {
				parts = append(parts, "env="+strings.Join(l.EnvReads, ","))
			}
			if l.Via != "" {
				parts = append(parts, "via="+l.Via)
			}
			fmt.Fprintf(out, "  %s (depth %d) %s\n", l.Repo, l.Depth, strings.Join(parts, " "))
			for i, s := range l.Sites {
				if servicesSites == 0 {
					break
				}
				if i == servicesSites {
					fmt.Fprintf(out, "      +%d more\n", len(l.Sites)-servicesSites)
					break
				}
				target := s.Target
				if s.Route != "" {
					target = fmt.Sprintf("%s %s (%s)", s.Target, s.Route, s.RouteFile)
				} else if s.Env != "" {
					target = fmt.Sprintf("%s via %s", s.Target, s.Env)
				}
				where := fmt.Sprintf("%s:%s", s.Repo, s.Path)
				if s.Line > 0 {
					where += fmt.Sprintf(":%d", s.Line)
				}
				fmt.Fprintf(out, "      %s %s -> %s [%s]\n", where, s.Symbol, target, s.Match)
			}
		}
		return nil
	},
}

func init() {
	queryServicesCmd.Flags().StringVar(&servicesConsumersOf, "consumers-of", "", "Member whose callers to list")
	queryServicesCmd.Flags().StringVar(&servicesCallsFrom, "calls-from", "", "Member whose calls to list")
	queryServicesCmd.Flags().IntVar(&servicesDepth, "depth", 1, "With --consumers-of, follow callers of callers")
	queryServicesCmd.Flags().IntVar(&servicesSites, "sites", 0, "Call sites to print per member")
	queryServicesCmd.Flags().BoolVar(&queryJSON, "json", false, "Emit JSON")
	queryCmd.AddCommand(queryServicesCmd)
}
