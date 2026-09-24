package kt02

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type column struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Required bool    `json:"required"`
	Identity string  `json:"identity"`
	Default  *string `json:"default_value"`
}
type constraint struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Definition string `json:"definition"`
}
type table struct {
	Name        string       `json:"name"`
	Columns     []column     `json:"columns"`
	Constraints []constraint `json:"constraints"`
}
type catalog struct {
	Tables []table `json:"tables"`
}
type descriptions struct {
	Tables  map[string]string `json:"tables"`
	Columns map[string]string `json:"columns"`
}

func dictionaryMarkdown(data catalog) (string, int, error) {
	raw, err := assets.ReadFile("assets/descriptions.json")
	if err != nil {
		return "", 0, err
	}
	var descriptions descriptions
	if err = json.Unmarshal(raw, &descriptions); err != nil {
		return "", 0, err
	}
	var b strings.Builder
	count := 0
	fmt.Fprintf(&b, "# Словарь данных\n\nТаблиц: %d. NN — NOT NULL. Типы и ключи взяты из каталога PostgreSQL после применения всех миграций.\n\n", len(data.Tables))
	for _, t := range data.Tables {
		description, ok := descriptions.Tables[t.Name]
		if !ok {
			return "", 0, fmt.Errorf("missing table description: %s", t.Name)
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", t.Name, description)
		for _, c := range t.Constraints {
			if c.Kind == "p" || c.Kind == "u" || c.Kind == "f" {
				fmt.Fprintln(&b, c.Definition+".")
			}
		}
		b.WriteString("\n| Атрибут | Тип / обязательность | Значение |\n|---|---|---|\n")
		for _, c := range t.Columns {
			desc, ok := descriptions.Columns[t.Name+"."+c.Name]
			if !ok {
				desc, ok = descriptions.Columns[c.Name]
			}
			if !ok {
				return "", 0, fmt.Errorf("missing column description: %s.%s", t.Name, c.Name)
			}
			if c.Identity != "" {
				desc += "; GENERATED ALWAYS AS IDENTITY"
			} else if c.Default != nil {
				desc += "; DEFAULT " + *c.Default
			}
			nullable := " NULL"
			if c.Required {
				nullable = " NN"
			}
			fmt.Fprintf(&b, "| %s | %s%s | %s |\n", c.Name, c.Type, nullable, desc)
			count++
		}
		b.WriteByte('\n')
	}
	return b.String(), count, nil
}
func (r *runner) dictionary() error {
	query, err := assets.ReadFile("assets/catalog.sql")
	if err != nil {
		return err
	}
	result, err := r.sql(r.database, string(query))
	if err != nil {
		return err
	}
	var data catalog
	if err = json.Unmarshal([]byte(result), &data); err != nil {
		return err
	}
	if len(data.Tables) == 0 {
		return fmt.Errorf("transport schema has no tables; apply migrations first")
	}
	md, count, err := dictionaryMarkdown(data)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(r.root, "docs/kt02/catalog.json"), data); err != nil {
		return err
	}
	if err = writeFile(filepath.Join(r.root, "docs/kt02/02-dictionary.md"), []byte(md)); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Dictionary: %d tables; %d columns\n", len(data.Tables), count)
	return nil
}
