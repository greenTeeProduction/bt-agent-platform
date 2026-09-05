package agent

import (
	"crypto/rand"
	"fmt"
	"os"
	"unicode"
	"unicode/utf8"
)

// ValidateName accepts existing letter/digit, dash, underscore and dotted agent
// identifiers (including Unicode letters). Names are single path components.
func ValidateName(name string) error {
	if name == "" || !utf8.ValidString(name) {
		return fmt.Errorf("invalid agent name %q", name)
	}
	for i, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || (r == '.' && i > 0) {
			continue
		}
		return fmt.Errorf("invalid agent name %q: use letters, digits, dashes, underscores or internal dots", name)
	}
	return nil
}

func regularDefinition(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("agent definition %q is not a regular file", name)
	}
	return nil
}

// WriteDefinitionFile atomically replaces a YAML definition beneath dir. A
// unique exclusive temporary file and rooted rename never write through a
// symlink or hard link, even if the destination changes after inspection.
func WriteDefinitionFile(dir, name string, data []byte) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	file := name + ".yaml"
	if err := regularDefinition(root, file); err != nil {
		return err
	}
	temp := ".agent-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, file)
}

// RemoveDefinitionFile removes a validated definition without following links.
// The legacy .yml fallback is used only when .yaml is absent.
func RemoveDefinitionFile(dir, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	file := name + ".yaml"
	if _, err := root.Lstat(file); os.IsNotExist(err) {
		file = name + ".yml"
	} else if err != nil {
		return err
	}
	if err := regularDefinition(root, file); err != nil {
		return err
	}
	return root.Remove(file)
}
