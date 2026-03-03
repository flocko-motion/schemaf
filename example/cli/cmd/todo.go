package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	basecli "atlas.local/base/cli"
	"github.com/spf13/cobra"
)

const defaultBackendURL = "http://localhost:7001"

// TodoSubcommandProvider returns the todo subcommand tree.
func TodoSubcommandProvider(ctx *basecli.Context) []*cobra.Command {
	todoCmd := &cobra.Command{
		Use:   "todo",
		Short: "Manage todos",
	}

	todoCmd.AddCommand(listCmd(ctx))
	todoCmd.AddCommand(createCmd(ctx))
	todoCmd.AddCommand(doneCmd(ctx))
	todoCmd.AddCommand(deleteCmd(ctx))

	return []*cobra.Command{todoCmd}
}

func backendURL() string {
	if u := ctx_backendURL; u != "" {
		return u
	}
	return defaultBackendURL
}

var ctx_backendURL string

type todoItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

func listCmd(ctx *basecli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all todos",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(backendURL() + "/api/todos")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)

			var todos []todoItem
			if err := json.Unmarshal(body, &todos); err != nil {
				return err
			}
			for _, t := range todos {
				mark := " "
				if t.Done {
					mark = "x"
				}
				fmt.Printf("[%s] %s  (%s)\n", mark, t.Text, t.ID)
			}
			return nil
		},
	}
}

func createCmd(ctx *basecli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "create <text>",
		Short: "Create a new todo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := fmt.Sprintf(`{"text":%q}`, args[0])
			resp, err := http.Post(backendURL()+"/api/todos", "application/json", strings.NewReader(payload))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)

			var t todoItem
			if err := json.Unmarshal(body, &t); err != nil {
				return err
			}
			fmt.Printf("Created: [%s] %s\n", t.ID, t.Text)
			return nil
		},
	}
}

func doneCmd(ctx *basecli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "done <id>",
		Short: "Mark a todo as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			// Fetch current state
			resp, err := http.Get(fmt.Sprintf("%s/api/todos/%s", backendURL(), id))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var t todoItem
			if err := json.Unmarshal(body, &t); err != nil {
				return err
			}

			// Update
			payload := fmt.Sprintf(`{"text":%q,"done":true}`, t.Text)
			req, _ := http.NewRequest(http.MethodPut,
				fmt.Sprintf("%s/api/todos/%s", backendURL(), id),
				strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{}
			resp2, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp2.Body.Close()
			fmt.Printf("Done: %s\n", id)
			return nil
		},
	}
}

func deleteCmd(ctx *basecli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a todo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			req, _ := http.NewRequest(http.MethodDelete,
				fmt.Sprintf("%s/api/todos/%s", backendURL(), id), nil)
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			fmt.Printf("Deleted: %s\n", id)
			return nil
		},
	}
}
