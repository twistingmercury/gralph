package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

// Run runs the loop over tl inside the run view and returns the run's exit
// code and one-line summary. ctx is the outside SIGINT/SIGTERM context. opts
// are extra program options, for tests.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile string, opts ...tea.ProgramOption) (exitCode int, summary string, err error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	p := tea.NewProgram(New(tl, cancel), append([]tea.ProgramOption{tea.WithoutSignalHandler()}, opts...)...)

	loopDone, progDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = looper.Run(runCtx, prompt, tl, tasksFile, func(e looper.Event) { p.Send(e) })
	}()
	go func() {
		select {
		case <-ctx.Done():
			p.Send(interruptedMsg{})
		case <-progDone:
		}
	}()

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
