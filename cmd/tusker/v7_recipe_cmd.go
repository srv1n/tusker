package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type v7VerificationRecipe struct {
	ID             string   `json:"id"`
	Title          string   `json:"title,omitempty"`
	Name           string   `json:"name,omitempty"`
	Commands       []string `json:"commands"`
	FileGlobs      []string `json:"file_globs,omitempty"`
	Domains        []string `json:"domains,omitempty"`
	Risks          []string `json:"risks,omitempty"`
	OwnershipScope string   `json:"ownership_scope,omitempty"`
	PackageScope   string   `json:"package_scope,omitempty"`
	ExpectedNoise  []string `json:"expected_noise,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

type v7VerificationRecipeReport struct {
	TaskID       string                 `json:"task_id"`
	TouchedFiles []string               `json:"touched_files"`
	Recipes      []v7VerificationRecipe `json:"recipes"`
	Policy       string                 `json:"policy"`
}

func proofV7RecipeCmd(args Args) error {
	return recipeV7Cmd(args)
}

func verifyV7RecipeCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"), args.String("_pos0"))
	return recipeV7Cmd(args)
}

func recipeV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID := firstNonEmpty(args.String("id"), args.String("_pos1"), args.String("_pos0"))
	if strings.TrimSpace(taskID) == "" || strings.EqualFold(taskID, "recipe") {
		return tuskerError(errorMissingArg, "proof recipe requires <task-id>")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	recipes, err := loadV7VerificationRecipes(vaultPath)
	if err != nil {
		return err
	}
	files := splitCSV(firstNonEmpty(args.String("files"), args.String("touched-files"), args.String("paths")))
	matched := matchV7VerificationRecipes(task, files, recipes)
	report := v7VerificationRecipeReport{
		TaskID:       taskID,
		TouchedFiles: files,
		Recipes:      matched,
		Policy:       v7VerificationRecipePolicyText(),
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	if len(matched) == 0 {
		fmt.Printf("No scoped verification recipe matched %s. Use broad validation or add tusker/verification-recipes.yaml.\n", taskID)
		return nil
	}
	fmt.Printf("Verification recipes for %s\n\n", taskID)
	for _, recipe := range matched {
		label := firstNonEmpty(recipe.ID, recipe.Name, recipe.Title, "unnamed recipe")
		fmt.Printf("- %s", label)
		if recipe.Reason != "" {
			fmt.Printf(" (%s)", recipe.Reason)
		}
		fmt.Println()
		for _, command := range recipe.Commands {
			fmt.Printf("  $ %s\n", command)
		}
		if recipe.OwnershipScope != "" {
			fmt.Printf("  scope: %s\n", recipe.OwnershipScope)
		}
		if recipe.PackageScope != "" {
			fmt.Printf("  package: %s\n", recipe.PackageScope)
		}
		for _, noise := range recipe.ExpectedNoise {
			fmt.Printf("  expected noise: %s\n", noise)
		}
	}
	fmt.Printf("\n%s\n", v7VerificationRecipePolicyText())
	return nil
}

func v7VerificationRecipePolicyText() string {
	return "Scoped recipes are acceptable when they cover the files, package, or domain owned by the task and expected noise is declared; use broad validation when shared contracts, generated artifacts, or cross-package behavior changed."
}

func loadV7VerificationRecipes(vaultPath string) ([]v7VerificationRecipe, error) {
	configPath := filepath.Join(vaultPath, "verification-recipes.yaml")
	raw, err := readText(configPath)
	if err != nil {
		return nil, nil
	}
	var data struct {
		Recipes []map[string]any `yaml:"recipes"`
	}
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		return nil, tuskerError(errorConfigInvalid, "failed to parse verification-recipes.yaml: "+err.Error(), withPath(configPath))
	}
	var recipes []v7VerificationRecipe
	for _, item := range data.Recipes {
		recipe := v7VerificationRecipe{
			ID:             firstNonEmpty(toString(item["id"]), toString(item["name"]), "unnamed recipe"),
			Title:          toString(item["title"]),
			Name:           firstNonEmpty(toString(item["name"]), toString(item["title"])),
			Commands:       normalizeList(item["commands"]),
			FileGlobs:      firstNonEmptyList(normalizeList(item["file_globs"]), normalizeList(item["globs"]), normalizeList(item["files"]), normalizeList(item["paths"])),
			Domains:        normalizeList(item["domains"]),
			Risks:          normalizeList(item["risks"]),
			OwnershipScope: firstNonEmpty(toString(item["ownership_scope"]), toString(item["scope"])),
			PackageScope:   firstNonEmpty(toString(item["package_scope"]), toString(item["package"])),
			ExpectedNoise:  firstNonEmptyList(normalizeList(item["expected_noise"]), normalizeList(item["noise"])),
		}
		if command := strings.TrimSpace(toString(item["command"])); command != "" {
			recipe.Commands = append([]string{command}, recipe.Commands...)
		}
		recipe.Commands = uniqueStrings(recipe.Commands)
		if len(recipe.Commands) == 0 {
			continue
		}
		recipes = append(recipes, recipe)
	}
	return recipes, nil
}

func matchV7VerificationRecipes(task Note, files []string, recipes []v7VerificationRecipe) []v7VerificationRecipe {
	taskDomains := lowerSet(normalizeList(task.Data["domains"]))
	taskRisk := strings.ToLower(stringField(task.Data, "risk"))
	var matched []v7VerificationRecipe
	for _, recipe := range recipes {
		reasons := []string{}
		if intersectsLower(taskDomains, recipe.Domains) {
			reasons = append(reasons, "domain")
		}
		if containsLower(recipe.Risks, taskRisk) {
			reasons = append(reasons, "risk")
		}
		if recipeMatchesFiles(recipe, files) {
			reasons = append(reasons, "files")
		}
		if len(recipe.Domains) == 0 && len(recipe.Risks) == 0 && len(recipe.FileGlobs) == 0 {
			reasons = append(reasons, "default")
		}
		if len(reasons) == 0 {
			continue
		}
		recipe.Reason = strings.Join(uniqueStrings(reasons), ",")
		matched = append(matched, recipe)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return firstNonEmpty(matched[i].ID, matched[i].Name) < firstNonEmpty(matched[j].ID, matched[j].Name)
	})
	return matched
}

func lowerSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func intersectsLower(set map[string]bool, values []string) bool {
	for _, value := range values {
		if set[strings.ToLower(strings.TrimSpace(value))] {
			return true
		}
	}
	return false
}

func containsLower(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func recipeMatchesFiles(recipe v7VerificationRecipe, files []string) bool {
	if len(recipe.FileGlobs) == 0 || len(files) == 0 {
		return false
	}
	for _, file := range files {
		file = filepath.ToSlash(strings.TrimSpace(file))
		for _, glob := range recipe.FileGlobs {
			if v7RecipeGlobMatch(filepath.ToSlash(strings.TrimSpace(glob)), file) {
				return true
			}
		}
	}
	return false
}

func v7RecipeGlobMatch(glob, file string) bool {
	if glob == "" || file == "" {
		return false
	}
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "/**")
		return file == prefix || strings.HasPrefix(file, prefix+"/")
	}
	if strings.Contains(glob, "/**/") {
		parts := strings.SplitN(glob, "/**/", 2)
		return strings.HasPrefix(file, parts[0]+"/") && strings.HasSuffix(file, parts[1])
	}
	if ok, _ := filepath.Match(glob, file); ok {
		return true
	}
	return strings.HasPrefix(file, strings.TrimSuffix(glob, "*"))
}
