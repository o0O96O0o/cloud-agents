package db

import "encoding/json"

// SkillMeta is the JSON payload stored in kinds.meta for skill resources.
type SkillMeta struct {
	Files []string `json:"files"`
}

// SkillFiles returns the list of relative file paths belonging to this skill.
// Defaults to ["SKILL.md"] for records created before multi-file support.
func (r *KindRecord) SkillFiles() []string {
	var m SkillMeta
	if err := json.Unmarshal(r.Meta, &m); err != nil || len(m.Files) == 0 {
		return []string{"SKILL.md"}
	}
	return m.Files
}
