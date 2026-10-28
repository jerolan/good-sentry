package native

import "slices"

// A conflicting image cannot be used to supply sections for this identity.
type pdbCompanion struct {
	sections  []pdbSection
	ambiguous bool
}

type pdbCompanions map[string]pdbCompanion

func (companions pdbCompanions) add(id string, sections []pdbSection) {
	key := DebugKey(id)
	if key == "" {
		return
	}
	if previous, exists := companions[key]; exists {
		if !slices.Equal(previous.sections, sections) {
			previous.ambiguous = true
			companions[key] = previous
		}
		return
	}
	companions[key] = pdbCompanion{sections: slices.Clone(sections)}
}

// Stage candidates until the entire archive has passed its validation.
func collectPDBCompanions(companions pdbCompanions, name string, data []byte, declaredID string) {
	local := pdbCompanions{}
	_, valid := scanDebugInput(name, data, func(_ string, data []byte) (DebugObject, bool) {
		id, sections, ok := parsePDBCompanionPE(data)
		if ok && (DebugKey(declaredID) == "" || DebugKey(declaredID) == DebugKey(id)) {
			local.add(id, sections)
		}
		return DebugObject{}, false
	})
	if !valid {
		return
	}
	for id, candidate := range local {
		companions.add(id, candidate.sections)
		if candidate.ambiguous {
			value := companions[id]
			value.ambiguous = true
			companions[id] = value
		}
	}
}
