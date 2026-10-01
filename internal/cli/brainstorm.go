// Brainstorm questions in the CLI (task #4901, spec §2.2): `task ask
// --brainstorm --recommend N` opens one, and `task brainstorm record` lets the
// orchestrator write down an answer the human gave in its terminal.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	brainstormFlagUsage = "вопрос шторма: нужны хотя бы два --option и --recommend; исход ответа считается автоматически"
	recommendFlagUsage  = "номер рекомендуемого варианта (1-based), обязателен для --brainstorm"
)

// validateBrainstormFlags rejects contradictory brainstorm flags before the
// daemon is called. A brainstorm question is a fork with at least two options
// and must recommend one of them — the recommendation is what the human's
// answer is measured against.
func validateBrainstormFlags(brainstorm, fyi bool, options []string, recommend int, usage string) error {
	fail := func(why string) error { return &usageError{message: usage + "\n" + why} }
	switch {
	case brainstorm && fyi:
		return fail("--brainstorm и --fyi несовместимы: вопрос шторма ждёт ответа человека")
	case !brainstorm && recommend != 0:
		return fail("--recommend бывает только у вопроса шторма (--brainstorm)")
	case brainstorm && len(options) < 2:
		return fail("у вопроса шторма должно быть хотя бы два варианта (--option): это развилка с рекомендацией")
	case brainstorm && recommend == 0:
		return fail("у вопроса шторма с вариантами укажите рекомендуемый: --recommend <n>")
	case brainstorm && (recommend < 0 || recommend > len(options)):
		return fail(fmt.Sprintf("--recommend должен быть от 1 до %d", len(options)))
	}
	return nil
}

// setBrainstorm marks a thread-opening request as a brainstorm question. A
// request without --brainstorm is left exactly as it was.
func setBrainstorm(req map[string]any, brainstorm bool, recommend int) {
	if !brainstorm {
		return
	}
	req["type"] = "brainstorm"
	if recommend > 0 {
		req["recommend"] = recommend
	}
}

// brainstormRecordOptions is the human's terminal answer as the orchestrator
// records it: the option they picked, their words, or both.
type brainstormRecordOptions struct {
	choose int
	body   string
}

func (o brainstormRecordOptions) validate(usage string) error {
	if o.choose < 0 || (o.choose == 0 && o.body == "") {
		return &usageError{message: usage}
	}
	return nil
}

func (o brainstormRecordOptions) requestBody() map[string]any {
	req := map[string]any{}
	if o.choose > 0 {
		req["choose"] = o.choose
	}
	if o.body != "" {
		req["body"] = o.body
	}
	return req
}

func newTaskBrainstormCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "brainstorm",
		Short: "Шторм задачи: запись ответов человека из терминала",
	}
	cmd.AddCommand(newTaskBrainstormRecordCmd())
	return cmd
}

// newTaskBrainstormRecordCmd builds "rocket task brainstorm record": the
// orchestrator writes down the answer the human gave in its terminal to an
// open brainstorm question — the human's words verbatim, never its own. The
// daemon accepts it only from the orchestrator of the question's task.
func newTaskBrainstormRecordCmd() *cobra.Command {
	var opts brainstormRecordOptions
	var taskFlag int64
	var file string

	const usage = "usage: rocket task brainstorm record <task-id>/Q<n>|<question-id> " +
		"[--choose <n>] [\"<текст человека дословно>\" | --file <path>] (нужно хотя бы одно) [--task <task-id>]"

	cmd := &cobra.Command{
		Use:   "record <task-id>/Q<n>|<question-id> [\"<текст человека>\"]",
		Short: "Записать ответ человека из терминала в вопрос шторма (только оркестратор задачи)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			hasBody := len(args) == 2
			if hasBody || file != "" {
				body, err := textBody(cmd, argAt(args, 1), hasBody, file, usage)
				if err != nil {
					return err
				}
				opts.body = body
			}
			if err := opts.validate(usage); err != nil {
				return err
			}
			id, err := resolveQuestionRef(args[0], taskFlag)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}
			var resp questionRow
			if err := c.Post(apiPath("v1", "questions", id, "brainstorm-record"), opts.requestBody(), &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderWriteResult("ответ человека из терминала записан, тред закрыт:", resp))
			if resp.Outcome != "" {
				cmd.Printf("исход: %s\n", resp.Outcome)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&opts.choose, "choose", 0, "номер варианта, который выбрал человек (1-based)")
	cmd.Flags().Int64Var(&taskFlag, "task", 0, taskFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с текстом человека ('-' — stdin)")
	return cmd
}
