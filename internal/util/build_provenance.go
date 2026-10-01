package util

import (
	"runtime/debug"
	"strings"
	"sync"
)

// BuildProvenance comes from the executing binary's Go VCS metadata. It never
// borrows a checkout's HEAD or a display-only ldflags revision stamp.
type BuildProvenance struct {
	Revision          string `json:"revision,omitempty"`
	CommitTime        string `json:"commit_time,omitempty"`
	Module            string `json:"module,omitempty"`
	Dirty             bool   `json:"dirty"`
	Known             bool   `json:"known"`
	LocalReplacements bool   `json:"local_replacements,omitempty"`
}

func BuildProvenanceFromInfo(info *debug.BuildInfo) BuildProvenance {
	var p BuildProvenance
	if info == nil {
		return p
	}
	p.Module = info.Main.Path
	var vcs, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs":
			vcs = setting.Value
		case "vcs.revision":
			p.Revision = setting.Value
		case "vcs.time":
			p.CommitTime = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	p.Dirty = modified != "false"
	for _, dependency := range info.Deps {
		if dependency.Replace != nil && dependency.Replace.Version == "" {
			p.LocalReplacements = true
		}
	}
	p.Known = vcs == "git" && (modified == "true" || modified == "false") &&
		(len(p.Revision) == 40 || len(p.Revision) == 64) && strings.Trim(p.Revision, "0123456789abcdef") == ""
	return p
}

// QualifiesCodeIdentity rejects missing, dirty and locally replaced sources.
// Operational display stamps may still be useful, but cannot establish adoption.
func (p BuildProvenance) QualifiesCodeIdentity() bool {
	return p.Known && !p.Dirty && !p.LocalReplacements && p.Module == "github.com/nico/go-bt-evolve" &&
		(len(p.Revision) == 40 || len(p.Revision) == 64) && strings.Trim(p.Revision, "0123456789abcdef") == ""
}

var binaryBuildProvenance = sync.OnceValue(func() BuildProvenance {
	info, _ := debug.ReadBuildInfo()
	return BuildProvenanceFromInfo(info)
})

func CurrentBuildProvenance() BuildProvenance { return binaryBuildProvenance() }
