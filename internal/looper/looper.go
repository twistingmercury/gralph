package looper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

var (
	promptMd    string
	prdMd       string
	progressTxt string
)

func Start(ctx context.Context, prompt, prd string) error {
	if _, err := os.Stat(prompt); err != nil && errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the file %s does not exist: %w", prompt, err)
	}
	if _, err := os.Stat(prd); err != nil && errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the file %s does not exist: %w", prd, err)
	}

	dir := filepath.Dir(prompt)

	progressTxt := path.Join(dir, "progress.txt")

	file, err := os.Create(progressTxt)
	if err != nil {
		return fmt.Errorf("could not create the required progress.txt file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if err := runLoop(ctx, prompt, prd, progressTxt); err != nil {
		return fmt.Errorf("could not create the required progress.txt file: %w", err)
	}

	return nil
}

func runLoop(ctx context.Context, prompt, prd, progress string) error {
	return errors.New("not implemented")
}

func invokeClaude(prompt, prd, progress string) error {
	return errors.New("not implemented")
}

func getFirstOpenItem() (string, error) {
	return "", errors.New("not implemented")
}

func completeFirstOpenItem() (string, error) {
	return "", errors.New("not implemented")
}

func abandonFirstOpenItem() (string, error) {
	return "", errors.New("not implemented")
}
