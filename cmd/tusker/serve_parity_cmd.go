package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func projectsIconCmd(args Args) error {
	if args.String("_pos0") != "set" {
		return tuskerError(errorInvalidArg, "Usage: tusker projects icon set <path>|--remove --id <PROJECT-ID>")
	}
	id := strings.TrimSpace(args.String("id"))
	if id == "" {
		return tuskerError(errorMissingArg, "projects icon set requires --id <PROJECT-ID>")
	}
	path, remove := args.String("_pos1"), args.Bool("remove")
	if remove == (path != "") {
		return tuskerError(errorInvalidArg, "provide exactly one icon path or --remove")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	project, err := projectByID(store, id)
	if err != nil {
		return err
	}
	if _, err := v7AgentDefaultActor(Args{"by": args.String("by"), "vault": project.VaultRoot}, "projects icon set"); err != nil {
		return err
	}
	body := serveActionBody{"clear": remove}
	if !remove {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) == 0 || len(data) > maxProjectIconBytes {
			return tuskerError(errorInvalidField, "icon must be under 512 KiB")
		}
		body["data"] = base64.StdEncoding.EncodeToString(data)
		if strings.EqualFold(filepath.Ext(path), ".svg") {
			body["mime"] = "image/svg+xml"
		}
	}
	result := saveProjectIcon((&serveServer{store: store}).projectIconGroupKey(id), id, body)
	if !result.OK {
		return tuskerError(errorInvalidField, result.Reason)
	}
	if args.Bool("json") {
		emitJSON(result)
	} else {
		fmt.Println(result.Reason)
	}
	return nil
}

func docsSaveCmd(args Args) error {
	ref := strings.TrimSpace(positionalPhrase(args))
	if ref == "" {
		return tuskerError(errorMissingArg, "docs save requires a subject or managed path")
	}
	if args.String("body-file") == "" && args.String("header-file") == "" {
		return tuskerError(errorMissingArg, "docs save requires --body-file or --header-file")
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if _, err := v7AgentDefaultActor(Args{"by": args.String("by"), "vault": vault}, "docs save"); err != nil {
		return err
	}
	repo := v7RepoRoot(vault)
	_, _, doc, _, raw, err := docsResolveDocument(repo, ref, false)
	if err != nil {
		return docsDiscoveryError(err)
	}
	req := serveDocgraphSaveRequest{BaseRev: firstNonEmpty(args.String("if-revision"), serveDocgraphRev(raw))}
	if path := args.String("body-file"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(data)
		req.Body = &body
	}
	if path := args.String("header-file"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var header map[string]any
		if err := yaml.Unmarshal(data, &header); err != nil {
			return err
		}
		if header == nil {
			return tuskerError(errorInvalidField, "header file must contain a YAML or JSON object")
		}
		req.Header, err = json.Marshal(header)
		if err != nil {
			return err
		}
	}
	status, result := saveDocgraphDoc(repo, doc.Subject, req)
	if status != http.StatusOK {
		encoded, _ := json.Marshal(result)
		return tuskerError(errorInvalidTransition, "docs save refused: "+string(encoded))
	}
	if args.Bool("json") {
		emitJSON(result)
	} else {
		fmt.Printf("Saved %s\n", doc.Subject)
	}
	return nil
}
