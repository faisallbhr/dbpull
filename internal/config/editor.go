package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
)

const (
	menuSource       = "source"
	menuSSH          = "ssh"
	menuTarget       = "target"
	menuSync         = "sync"
	menuSave         = "save"
	menuExit         = "exit"
	menuBack         = "back"
	menuAdd          = "add"
	menuRemove       = "remove"
	menuYes          = "yes"
	menuNo           = "no"
	menuCancel       = "cancel"
	menuExcludeTable = "exclude_tables"
	menuExcludeData  = "exclude_data"
)

type EditorOptions struct {
	Path         string
	Force        bool
	RequireNew   bool
	CreateParent bool
	Input        io.Reader
	Output       io.Writer
}

type UI interface {
	Select(title string, options []MenuOption) (string, error)
	Input(title string, value string, password bool) (string, error)
}

type MenuOption struct {
	Label string
	Value string
}

type SessionResult struct {
	Config  Config
	Path    string
	Saved   bool
	Aborted bool
	Loaded  bool
}

func RunEditor(opts EditorOptions) (SessionResult, error) {
	return runEditor(newHuhUI(opts.Input, opts.Output), opts)
}

func runEditor(ui UI, opts EditorOptions) (SessionResult, error) {
	path := resolvePath(opts.Path)

	initial, loaded, err := loadInitialConfig(path, opts)
	if err != nil {
		return SessionResult{}, err
	}

	current := initial
	saved := initial

	for {
		choice, err := ui.Select("DBPull Configuration", []MenuOption{
			{Label: "Source Database", Value: menuSource},
			{Label: "SSH", Value: menuSSH},
			{Label: "Target Database", Value: menuTarget},
			{Label: "Synchronization", Value: menuSync},
			{Label: "Save", Value: menuSave},
			{Label: "Exit", Value: menuExit},
		})
		if err != nil {
			return SessionResult{}, err
		}

		switch choice {
		case menuSource:
			if err := editSourceSection(ui, &current); err != nil {
				return SessionResult{}, err
			}
		case menuSSH:
			if err := editSSHSection(ui, &current); err != nil {
				return SessionResult{}, err
			}
		case menuTarget:
			if err := editTargetSection(ui, &current); err != nil {
				return SessionResult{}, err
			}
		case menuSync:
			if err := editSyncSection(ui, &current); err != nil {
				return SessionResult{}, err
			}
		case menuSave:
			return persistSession(path, current, opts.CreateParent, loaded)
		case menuExit:
			if !reflect.DeepEqual(current, saved) {
				exitChoice, err := ui.Select("Configuration has changed.\n\nSave changes?", []MenuOption{
					{Label: "Yes", Value: menuYes},
					{Label: "No", Value: menuNo},
					{Label: "Cancel", Value: menuCancel},
				})
				if err != nil {
					return SessionResult{}, err
				}

				switch exitChoice {
				case menuYes:
					return persistSession(path, current, opts.CreateParent, loaded)
				case menuNo:
					return SessionResult{
						Config:  saved,
						Path:    path,
						Saved:   false,
						Loaded:  loaded,
						Aborted: true,
					}, nil
				case menuCancel:
					continue
				}
			}

			return SessionResult{
				Config:  saved,
				Path:    path,
				Saved:   false,
				Loaded:  loaded,
				Aborted: true,
			}, nil
		}
	}
}

func persistSession(path string, cfg Config, createParent bool, loaded bool) (SessionResult, error) {
	loadedCfg, absPath, err := save(path, cfg, createParent)
	if err != nil {
		return SessionResult{}, err
	}

	return SessionResult{
		Config:  loadedCfg,
		Path:    absPath,
		Saved:   true,
		Loaded:  loaded,
		Aborted: false,
	}, nil
}

func loadInitialConfig(path string, opts EditorOptions) (Config, bool, error) {
	if opts.RequireNew {
		if !opts.Force {
			if _, err := os.Stat(path); err == nil {
				return Config{}, false, fmt.Errorf("config file %q already exists; use --force to overwrite", path)
			} else if !os.IsNotExist(err) {
				return Config{}, false, fmt.Errorf("check config file %q: %w", path, err)
			}
		}
		return DefaultInitConfig(), false, nil
	}

	cfg, err := Load(path)
	if err == nil {
		return cfg, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return DefaultInitConfig(), false, nil
	}
	return Config{}, false, err
}

func editSourceSection(ui UI, cfg *Config) error {
	value, err := ui.Input("Source Database\n\nDatabase", cfg.Source.Database, false)
	if err != nil {
		return err
	}
	cfg.Source.Database = value

	value, err = ui.Input("Source Database\n\nUsername", cfg.Source.Username, false)
	if err != nil {
		return err
	}
	cfg.Source.Username = value

	value, err = ui.Input("Source Database\n\nPassword", cfg.Source.Password, true)
	if err != nil {
		return err
	}
	cfg.Source.Password = value

	return nil
}

func editSSHSection(ui UI, cfg *Config) error {
	value, err := ui.Input("SSH\n\nHost", cfg.SSH.Host, false)
	if err != nil {
		return err
	}
	cfg.SSH.Host = value

	value, err = ui.Input("SSH\n\nPort", strconv.Itoa(cfg.SSH.Port), false)
	if err != nil {
		return err
	}
	cfg.SSH.Port = atoiOrZero(value)

	value, err = ui.Input("SSH\n\nUser", cfg.SSH.User, false)
	if err != nil {
		return err
	}
	cfg.SSH.User = value

	value, err = ui.Input("SSH\n\nPrivate Key", cfg.SSH.PrivateKey, false)
	if err != nil {
		return err
	}
	cfg.SSH.PrivateKey = value

	return nil
}

func editTargetSection(ui UI, cfg *Config) error {
	value, err := ui.Input("Target Database\n\nHost", cfg.Target.Host, false)
	if err != nil {
		return err
	}
	cfg.Target.Host = value

	value, err = ui.Input("Target Database\n\nPort", strconv.Itoa(cfg.Target.Port), false)
	if err != nil {
		return err
	}
	cfg.Target.Port = atoiOrZero(value)

	value, err = ui.Input("Target Database\n\nDatabase", cfg.Target.Database, false)
	if err != nil {
		return err
	}
	cfg.Target.Database = value

	value, err = ui.Input("Target Database\n\nUsername", cfg.Target.Username, false)
	if err != nil {
		return err
	}
	cfg.Target.Username = value

	value, err = ui.Input("Target Database\n\nPassword", cfg.Target.Password, true)
	if err != nil {
		return err
	}
	cfg.Target.Password = value

	return nil
}

func editSyncSection(ui UI, cfg *Config) error {
	for {
		choice, err := ui.Select("Synchronization", []MenuOption{
			{Label: fmt.Sprintf("Batch Size (%d)", cfg.Sync.BatchSize), Value: "batch"},
			{Label: "Exclude Tables", Value: menuExcludeTable},
			{Label: "Exclude Data", Value: menuExcludeData},
			{Label: "Back", Value: menuBack},
		})
		if err != nil {
			return err
		}

		switch choice {
		case "batch":
			value, err := ui.Input("Synchronization\n\nBatch Size", strconv.Itoa(cfg.Sync.BatchSize), false)
			if err != nil {
				return err
			}
			cfg.Sync.BatchSize = atoiOrZero(value)
		case menuExcludeTable:
			items, err := editPatternList(ui, "Exclude Tables", cfg.Sync.ExcludeTables)
			if err != nil {
				return err
			}
			cfg.Sync.ExcludeTables = items
		case menuExcludeData:
			items, err := editPatternList(ui, "Exclude Data", cfg.Sync.ExcludeData)
			if err != nil {
				return err
			}
			cfg.Sync.ExcludeData = items
		case menuBack:
			return nil
		}
	}
}

func editPatternList(ui UI, title string, items []string) ([]string, error) {
	current := append([]string(nil), items...)

	for {
		choice, err := ui.Select(patternListTitle(title, current), []MenuOption{
			{Label: "Add", Value: menuAdd},
			{Label: "Remove", Value: menuRemove},
			{Label: "Back", Value: menuBack},
		})
		if err != nil {
			return nil, err
		}

		switch choice {
		case menuAdd:
			value, err := ui.Input(title+"\n\nAdd pattern", "", false)
			if err != nil {
				return nil, err
			}
			current = normalizePatterns(append(current, value))
		case menuRemove:
			if len(current) == 0 {
				continue
			}
			options := make([]MenuOption, 0, len(current)+1)
			for _, item := range current {
				options = append(options, MenuOption{Label: item, Value: item})
			}
			options = append(options, MenuOption{Label: "Back", Value: menuBack})

			value, err := ui.Select(title+"\n\nRemove pattern", options)
			if err != nil {
				return nil, err
			}
			if value == menuBack {
				continue
			}
			current = removePattern(current, value)
		case menuBack:
			return current, nil
		}
	}
}

func patternListTitle(title string, items []string) string {
	var builder strings.Builder
	builder.WriteString(title)
	if len(items) == 0 {
		builder.WriteString("\n\n(no entries)")
		return builder.String()
	}

	for _, item := range items {
		builder.WriteString("\n\n")
		builder.WriteString("✓ ")
		builder.WriteString(item)
	}

	return builder.String()
}

func removePattern(items []string, value string) []string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if item == value {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func normalizePatterns(items []string) []string {
	normalized := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

type huhUI struct {
	input  io.Reader
	output io.Writer
}

func newHuhUI(input io.Reader, output io.Writer) UI {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	return huhUI{input: input, output: output}
}

func (u huhUI) Select(title string, options []MenuOption) (string, error) {
	var value string
	field := huh.NewSelect[string]().Title(title).Value(&value).Options(toHuhOptions(options)...)
	form := huh.NewForm(huh.NewGroup(field)).WithInput(u.input).WithOutput(u.output)
	if err := form.Run(); err != nil {
		return "", err
	}
	return value, nil
}

func (u huhUI) Input(title string, value string, password bool) (string, error) {
	field := huh.NewInput().Title(title).Value(&value).Password(password)
	form := huh.NewForm(huh.NewGroup(field)).WithInput(u.input).WithOutput(u.output)
	if err := form.Run(); err != nil {
		return "", err
	}
	return value, nil
}

func toHuhOptions(options []MenuOption) []huh.Option[string] {
	converted := make([]huh.Option[string], 0, len(options))
	for _, option := range options {
		converted = append(converted, huh.NewOption(option.Label, option.Value))
	}
	return converted
}

func atoiOrZero(value string) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return number
}
