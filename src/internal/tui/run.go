package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

// Run runs the loop over tl inside the run view and returns the run's exit
// code and one-line summary. ctx is the outside SIGINT/SIGTERM context.
// sessionArgs and repo are as for looper.Run. opts are extra program options,
// for tests.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *looper.Repo, opts ...tea.ProgramOption) (exitCode int, summary string, err error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	model := New(tl, cancel)
	programOpts := append([]tea.ProgramOption{tea.WithoutSignalHandler()}, opts...)
	p := tea.NewProgram(model, programOpts...)

	loopDone, progDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = looper.Run(runCtx, prompt, tl, tasksFile, gateTimeout, sessionArgs, repo, func(e looper.Event) { p.Send(e) })
	}()
	go forwardSignal(ctx, p, progDone)

	final, err := p.Run()
	close(progDone)
	if err != nil {
		cancel()
		<-loopDone
		return 0, "", err
	}

	<-loopDone
	m := final.(Model)
	return m.ExitCode(), m.Summary(), nil
}

// forwardSignal exists because the program runs without Bubble Tea's own
// signal handler: an outside stop cancels ctx and nothing else would tell the
// view, so it has to be sent to the program. progDone ends the goroutine when
// the program finishes first.
func forwardSignal(ctx context.Context, p *tea.Program, progDone <-chan struct{}) {
	select {
	case <-ctx.Done():
		p.Send(interruptedMsg{})
	case <-progDone:
	}
}
