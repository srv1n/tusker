package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var deliveryCrossScopeAfterResolution func()
var deliveryCrossScopeAfterIndexLoad func()

// deliveryResolveCrossScopeDependencies resolves source identities while the
// vault-wide material epoch is held.  It never consults plan files or performs
// recursive imports: an imported, open-wave producer is the only valid target.
func deliveryResolveCrossScopeDependencies(vault string, idx v7Index, plan deliveryPlan, mapping map[string]string) (map[string][]deliveryCrossScopeDependency, func() error, func(string, []byte), []string, error) {
	if err := validateDeliveryCrossScopeIndex(idx, v7ProjectID(vault)); err != nil {
		return nil, nil, nil, nil, err
	}
	resolved := map[string][]deliveryCrossScopeDependency{}
	for _, consumer := range plan.Tasks {
		resolvedTargets := map[string]bool{}
		for _, dep := range consumer.Dependencies {
			scope := strings.TrimSpace(dep.scope)
			if scope == "" {
				continue
			}
			var semanticMatches, matches []Note
			for _, candidate := range idx.Tasks {
				if stringField(candidate.Data, "delivery_plan_scope") != scope ||
					stringField(candidate.Data, "delivery_source_key") != dep.Task {
					continue
				}
				semanticMatches = append(semanticMatches, candidate)
				if stringField(candidate.Data, "project") != v7ProjectID(vault) {
					continue
				}
				if deliveryCrossScopeProducerCurrent(candidate, idx) {
					matches = append(matches, candidate)
				}
			}
			if len(semanticMatches) > 1 {
				return nil, nil, nil, nil, tuskerError(errorInvalidTransition, fmt.Sprintf("CROSS_SCOPE_PRODUCER_DUPLICATE scope=%s key=%s consumer=%s", scope, dep.Task, consumer.SourceKey))
			}
			if len(matches) != 1 {
				code := "CROSS_SCOPE_PRODUCER_MISSING"
				if len(semanticMatches) == 1 {
					candidate := semanticMatches[0]
					switch {
					case stringField(candidate.Data, "project") != v7ProjectID(vault):
						code = "CROSS_SCOPE_PRODUCER_FOREIGN_VAULT"
					case stringField(candidate.Data, "discarded_at") != "" || stringField(candidate.Data, "status") == "cancelled":
						code = "CROSS_SCOPE_PRODUCER_DISCARDED"
					case stringField(candidate.Data, "status") == "superseded":
						code = "CROSS_SCOPE_PRODUCER_OBSOLETE"
					default:
						code = "CROSS_SCOPE_PRODUCER_REMOVED"
					}
				}
				return nil, nil, nil, nil, tuskerError(errorInvalidTransition,
					fmt.Sprintf("%s scope=%s key=%s consumer=%s; import exactly one current producer before this consumer", code, scope, dep.Task, consumer.SourceKey))
			}
			producer := matches[0]
			producerID := stringField(producer.Data, "id")
			if resolvedTargets[producerID] {
				return nil, nil, nil, nil, tuskerError(errorInvalidTransition, "CROSS_SCOPE_DUPLICATE_TARGET consumer="+consumer.SourceKey+" target="+producerID)
			}
			resolvedTargets[producerID] = true
			fingerprint := stringField(producer.Data, "delivery_contract_fingerprint")
			if fingerprint == "" {
				return nil, nil, nil, nil, tuskerError(errorInvalidTransition,
					fmt.Sprintf("CROSS_SCOPE_PRODUCER_STALE scope=%s key=%s consumer=%s; repair and re-import the producer", scope, dep.Task, consumer.SourceKey))
			}
			resolved[consumer.SourceKey] = append(resolved[consumer.SourceKey], deliveryCrossScopeDependency{
				Scope: scope, Task: dep.Task, TaskID: producerID, Kind: "hard", TargetContractFingerprint: fingerprint,
			})
		}
	}
	if err := validateDeliveryCrossScopeGraph(idx, plan, mapping, resolved); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := validateDeliveryCrossScopeInboundRemoval(idx, plan, mapping); err != nil {
		return nil, nil, nil, nil, err
	}
	snapshot, advance, paths, err := deliveryCrossScopeSnapshot(vault, idx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return resolved, snapshot, advance, paths, nil
}

// The epoch serializes cooperative writers, but raw edits can bypass it.
// Snapshot every graph document used by resolution and reject any changed or
// invalid state revision immediately before the atomic writer takes preimages.
func deliveryCrossScopeSnapshot(vault string, idx v7Index) (func() error, func(string, []byte), []string, error) {
	snapshots := map[string][]byte{}
	add := func(note Note) error {
		raw, err := os.ReadFile(note.AbsolutePath)
		if err != nil {
			return tuskerError(errorInvalidTransition, "CROSS_SCOPE_SNAPSHOT_READ_FAILED path="+note.AbsolutePath)
		}
		data, body, err := parseFrontmatter(string(raw))
		strictRev := effectiveV7Kind(note.Data) == "task" || effectiveV7Kind(note.Data) == "wave"
		if err != nil || !reflect.DeepEqual(data, note.Data) || v7CanonicalBody(body) != v7CanonicalBody(note.Body) || stringField(data, "state_rev") != stringField(note.Data, "state_rev") || (strictRev && !v7StateRevMatches(data, body, stringField(data, "state_rev"))) {
			return tuskerError(errorInvalidTransition, "CROSS_SCOPE_STATE_REV_INVALID path="+note.AbsolutePath)
		}
		snapshots[note.AbsolutePath] = raw
		return nil
	}
	for _, notes := range []map[string]Note{idx.Tasks, idx.Waves, idx.Gates, idx.Epics} {
		for _, note := range notes {
			if err := add(note); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	namespaces := map[string][]string{}
	for _, kind := range []string{"tasks", "waves", "gates", "epics"} {
		dir := filepath.Join(vault, "work", kind)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			// An absent standard namespace and an existing empty namespace are
			// graph-equivalent. Dry-run must not create scaffolding merely to
			// inspect an otherwise valid vault.
			namespaces[dir] = []string{}
			continue
		}
		if err != nil {
			return nil, nil, nil, tuskerError(errorInvalidTransition, "CROSS_SCOPE_NAMESPACE_READ_FAILED path="+dir)
		}
		for _, entry := range entries {
			namespaces[dir] = append(namespaces[dir], entry.Name())
		}
		sort.Strings(namespaces[dir])
	}
	verify := func() error {
		for path, expected := range snapshots {
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, expected) {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_EPOCH_STALE path="+path+"; retry import from a fresh material epoch")
			}
		}
		for dir, expected := range namespaces {
			entries, err := os.ReadDir(dir)
			if os.IsNotExist(err) && len(expected) == 0 {
				continue
			}
			if err != nil {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_NAMESPACE_READ_FAILED path="+dir)
			}
			var current []string
			for _, entry := range entries {
				current = append(current, entry.Name())
			}
			sort.Strings(current)
			if strings.Join(current, "\x00") != strings.Join(expected, "\x00") {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_NAMESPACE_STALE path="+dir)
			}
		}
		return nil
	}
	advance := func(path string, raw []byte) {
		snapshots[path] = append([]byte(nil), raw...)
		dir := filepath.Dir(path)
		if _, ok := namespaces[dir]; ok {
			name := filepath.Base(path)
			if _, exists := makeSet(namespaces[dir]...)[name]; !exists {
				namespaces[dir] = append(namespaces[dir], name)
				sort.Strings(namespaces[dir])
			}
		}
	}
	paths := make([]string, 0, len(snapshots))
	for path := range snapshots {
		paths = append(paths, path)
	}
	return verify, advance, paths, nil
}

func deliveryCrossScopeProducerCurrent(task Note, idx v7Index) bool {
	status := strings.ToLower(stringField(task.Data, "status"))
	if status == "cancelled" || status == "superseded" || stringField(task.Data, "discarded_at") != "" {
		return false
	}
	waveID := stringField(task.Data, "wave")
	wave, ok := idx.Waves[waveID]
	// A completed producer can live in a landed wave. Membership is the
	// lifecycle provenance; an importer removes a source key by removing that
	// membership, not by merely closing the wave.
	if !ok {
		return false
	}
	for _, member := range normalizeList(wave.Data["members"]) {
		if member == stringField(task.Data, "id") {
			return true
		}
	}
	return false
}

func deliveryCrossScopeProjections(task Note) ([]deliveryCrossScopeDependency, error) {
	if task.Data["delivery_cross_scope_dependencies"] == nil {
		return nil, nil
	}
	raw, err := yaml.Marshal(task.Data["delivery_cross_scope_dependencies"])
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, err
	}
	seq := deliveryYAMLMapping(&node)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("projection must be a sequence")
	}
	seenSemantic, seenTarget := map[string]bool{}, map[string]bool{}
	var projections []deliveryCrossScopeDependency
	for _, item := range seq.Content {
		if err := deliveryKnownYAMLFields(item, map[string]bool{"scope": true, "task": true, "task_id": true, "kind": true, "target_contract_fingerprint": true}); err != nil {
			return nil, err
		}
		fields := map[string]string{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, value := item.Content[i].Value, item.Content[i+1]
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return nil, fmt.Errorf("projection %s must be a string", key)
			}
			fields[key] = value.Value
		}
		for _, key := range []string{"scope", "task", "task_id", "kind", "target_contract_fingerprint"} {
			if fields[key] == "" {
				return nil, fmt.Errorf("projection missing %s", key)
			}
		}
		semantic, target := fields["scope"]+"\x00"+fields["task"], fields["task_id"]
		if seenSemantic[semantic] || seenTarget[target] {
			return nil, fmt.Errorf("duplicate projection semantic target")
		}
		seenSemantic[semantic], seenTarget[target] = true, true
		projections = append(projections, deliveryCrossScopeDependency{Scope: fields["scope"], Task: fields["task"], TaskID: fields["task_id"], Kind: fields["kind"], TargetContractFingerprint: fields["target_contract_fingerprint"]})
	}
	return projections, nil
}

func deliveryCrossScopeProjectionValue(projections []deliveryCrossScopeDependency) []any {
	out := make([]any, 0, len(projections))
	for _, p := range projections {
		// Use ordinary maps so state_rev is stable after frontmatter parse. The
		// document serializer deterministically orders map fields.
		out = append(out, map[string]any{"scope": p.Scope, "task": p.Task, "task_id": p.TaskID, "kind": p.Kind, "target_contract_fingerprint": p.TargetContractFingerprint})
	}
	return out
}

// validateDeliveryCrossScopeIndex refuses drift rather than using projection
// data as a hint to rebind.  This is intentionally global: corruption in one
// durable edge makes a supposedly atomic graph transaction untrustworthy.
func validateDeliveryCrossScopeIndex(idx v7Index, project string) error {
	for _, consumer := range idx.Tasks {
		projections, err := deliveryCrossScopeProjections(consumer)
		if err != nil {
			return tuskerError(errorInvalidTransition, "CROSS_SCOPE_PROJECTION_INVALID consumer="+stringField(consumer.Data, "id")+"; repair the durable projection")
		}
		projected := map[string]int{}
		for _, p := range projections {
			consumerID := stringField(consumer.Data, "id")
			if p.Scope == stringField(consumer.Data, "delivery_plan_scope") {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_LOCAL_PROJECTION consumer="+consumerID+"; local edges must not have cross-scope projections")
			}
			if p.Scope == "" || p.Task == "" || p.TaskID == "" || p.Kind != "hard" || p.TargetContractFingerprint == "" || p.TargetContractFingerprint != strings.TrimSpace(p.TargetContractFingerprint) {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_PROJECTION_INVALID consumer="+consumerID+"; repair every projection field")
			}
			matches := 0
			for _, edge := range v7TaskDependencyEdges(consumer, idx) {
				if edge.ID == p.TaskID && edge.Hardness == v7DependencyHardnessHard {
					matches++
				}
			}
			if matches != 1 {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_EDGE_DRIFT consumer="+consumerID+" target="+p.TaskID+"; restore the ordinary hard edge")
			}
			projected[p.TaskID]++
			producer, ok := idx.Tasks[p.TaskID]
			if !ok || stringField(producer.Data, "project") != project || stringField(producer.Data, "delivery_plan_scope") != p.Scope || stringField(producer.Data, "delivery_source_key") != p.Task || stringField(producer.Data, "delivery_contract_fingerprint") != p.TargetContractFingerprint || !deliveryCrossScopeProducerCurrent(producer, idx) {
				return tuskerError(errorInvalidTransition, fmt.Sprintf("CROSS_SCOPE_TARGET_DRIFT scope=%s key=%s consumer=%s; re-import the original producer and consumer together", p.Scope, p.Task, consumerID))
			}
		}
		consumerScope := stringField(consumer.Data, "delivery_plan_scope")
		for _, edge := range v7TaskDependencyEdges(consumer, idx) {
			producer, ok := idx.Tasks[edge.ID]
			if !ok || stringField(producer.Data, "delivery_plan_scope") == "" || stringField(producer.Data, "delivery_plan_scope") == consumerScope {
				continue
			}
			if edge.Hardness != v7DependencyHardnessHard || projected[edge.ID] != 1 {
				return tuskerError(errorInvalidTransition, "CROSS_SCOPE_UNPROJECTED_EDGE consumer="+stringField(consumer.Data, "id")+" target="+edge.ID+"; restore the exact projection and hard edge")
			}
		}
	}
	return nil
}

func validateDeliveryCrossScopeGraph(idx v7Index, plan deliveryPlan, mapping map[string]string, resolved map[string][]deliveryCrossScopeDependency) error {
	graph := map[string][]string{}
	for id, task := range idx.Tasks {
		for _, edge := range v7TaskDependencyEdges(task, idx) {
			graph[id] = append(graph[id], edge.ID)
		}
	}
	for _, task := range plan.Tasks {
		id := mapping[task.SourceKey]
		var deps []string
		for _, dep := range task.Dependencies {
			id := mapping[dep.Task]
			if dep.scope != "" {
				for _, p := range resolved[task.SourceKey] {
					if p.Scope == dep.scope && p.Task == dep.Task {
						id = p.TaskID
						break
					}
				}
			}
			deps = append(deps, id)
		}
		graph[id] = deps
	}
	visiting, visited := map[string]int{}, map[string]bool{}
	var stack []string
	var cycle []string
	var visit func(string) bool
	visit = func(id string) bool {
		if at, ok := visiting[id]; ok {
			cycle = append(append([]string{}, stack[at:]...), id)
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = len(stack)
		stack = append(stack, id)
		deps := append([]string{}, graph[id]...)
		sort.Strings(deps)
		for _, dep := range deps {
			if visit(dep) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		delete(visiting, id)
		visited[id] = true
		return false
	}
	ids := make([]string, 0, len(graph))
	for id := range graph {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if visit(id) {
			return tuskerError(errorInvalidArg, "CROSS_SCOPE_GLOBAL_CYCLE path="+strings.Join(cycle, "->")+"; remove the complete durable dependency cycle before import")
		}
	}
	return nil
}

func validateDeliveryCrossScopeInboundRemoval(idx v7Index, plan deliveryPlan, mapping map[string]string) error {
	wanted := map[string]bool{}
	for _, task := range plan.Tasks {
		wanted[mapping[task.SourceKey]] = true
	}
	var affected []string
	for id, producer := range idx.Tasks {
		if stringField(producer.Data, "delivery_plan_scope") != plan.Scope || wanted[id] {
			continue
		}
		for _, consumer := range idx.Tasks {
			if stringField(consumer.Data, "delivery_plan_scope") == plan.Scope {
				continue
			}
			if v7TaskDependsOnID(consumer, id, idx) {
				affected = append(affected, id+"->"+stringField(consumer.Data, "id"))
			}
		}
	}
	if len(affected) > 0 {
		sort.Strings(affected)
		return tuskerError(errorInvalidTransition, "CROSS_SCOPE_INBOUND_REMOVAL consumers="+strings.Join(affected, ",")+"; keep the producer source key or explicitly detach consumers first")
	}
	return nil
}

// deliveryRefreshInboundProjectionWrites carries a producer contract rewrite
// through every already-resolved inbound consumer in the same transaction. It
// never searches by source key to heal drift: the stored task ID must still
// identify the exact semantic producer or the entire import refuses.
func deliveryRefreshInboundProjectionWrites(vault string, idx v7Index, plan deliveryPlan, report deliveryImportReport, writes map[string]string, now, actor string) error {
	if plan.v2 == nil {
		return nil
	}
	nextFingerprints := map[string]string{}
	for _, task := range plan.Tasks {
		nextFingerprints[report.TaskMapping[task.SourceKey]] = deliveryV2TaskFingerprint(task, plan.v2.HumanGates)
	}
	for _, consumer := range idx.Tasks {
		projections, err := deliveryCrossScopeProjections(consumer)
		if err != nil {
			return tuskerError(errorInvalidTransition, "CROSS_SCOPE_PROJECTION_INVALID consumer="+stringField(consumer.Data, "id")+"; repair before producer rewrite")
		}
		changed := false
		for i := range projections {
			p := &projections[i]
			next, rewriting := nextFingerprints[p.TaskID]
			if !rewriting || next == p.TargetContractFingerprint {
				continue
			}
			producer, ok := idx.Tasks[p.TaskID]
			if !ok || stringField(producer.Data, "delivery_plan_scope") != p.Scope || stringField(producer.Data, "delivery_source_key") != p.Task {
				return tuskerError(errorInvalidTransition, fmt.Sprintf("CROSS_SCOPE_TARGET_DRIFT scope=%s key=%s consumer=%s; refusing producer rewrite", p.Scope, p.Task, stringField(consumer.Data, "id")))
			}
			p.TargetContractFingerprint = next
			changed = true
		}
		if !changed {
			continue
		}
		data, body, err := parseFrontmatterMustRead(consumer.AbsolutePath)
		if err != nil {
			return err
		}
		consumerID := stringField(consumer.Data, "id")
		if stringField(data, "state_rev") != stringField(consumer.Data, "state_rev") || !v7StateRevMatches(data, body, stringField(data, "state_rev")) {
			return tuskerError(errorInvalidTransition, "CROSS_SCOPE_INBOUND_CAS_CONFLICT consumer="+consumerID+"; retry the producer rewrite from a fresh material epoch")
		}
		if stringField(data, "status") != "backlog" || stringField(data, "readiness") != "held" {
			return tuskerError(errorInvalidTransition, "CROSS_SCOPE_INBOUND_CONSUMER_PROGRESS consumer="+consumerID+"; use an explicit rework/control transition before rewriting its producer contract")
		}
		data["delivery_cross_scope_dependencies"] = deliveryCrossScopeProjectionValue(projections)
		data["updated_at"], data["updated_by"] = now, actor
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			return err
		}
		writes[consumer.AbsolutePath] = content
	}
	return nil
}
