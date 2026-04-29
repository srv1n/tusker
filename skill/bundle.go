package skill

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed SKILL.md README.md LICENSE V1_DESIGN.md references docs agents assets
var embedded embed.FS

type AssetEntry struct {
	Key      string
	Relative string
	Content  string
}

type PayloadEntry struct {
	Relative string
	Content  string
}

func GetAsset(key string) (string, error) {
	raw, err := fs.ReadFile(embedded, path.Join("assets", key))
	if err != nil {
		return "", fmt.Errorf("embedded asset not found: %s", key)
	}
	return string(raw), nil
}

func AssetEntries(prefix string) ([]AssetEntry, error) {
	normalized := prefix
	if !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}

	var out []AssetEntry
	err := fs.WalkDir(embedded, "assets", func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		key := strings.TrimPrefix(current, "assets/")
		if !strings.HasPrefix(key, normalized) {
			return nil
		}
		raw, readErr := fs.ReadFile(embedded, current)
		if readErr != nil {
			return readErr
		}
		out = append(out, AssetEntry{
			Key:      key,
			Relative: strings.TrimPrefix(key, normalized),
			Content:  string(raw),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func PayloadEntries() ([]PayloadEntry, error) {
	var entries []PayloadEntry
	err := fs.WalkDir(embedded, ".", func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, readErr := fs.ReadFile(embedded, current)
		if readErr != nil {
			return readErr
		}
		entries = append(entries, PayloadEntry{
			Relative: path.Clean(strings.TrimPrefix(current, "./")),
			Content:  string(raw),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Relative < entries[j].Relative
	})
	return entries, nil
}
