package agent

// devCommands and devStreams replace commands and streams of the registry. Only the development simulator
// (build tag "sim") fills them, so that every tab of the panel can be looked at without a real Linux host;
// a normal build leaves them empty.
var (
	devCommands map[string]CmdFunc
	devStreams  map[string]StreamFunc
)

func (r *Registry) applyDevOverrides() {
	for name, fn := range devCommands {
		r.cmds[name] = fn
	}
	for name, fn := range devStreams {
		r.streams[name] = fn
	}
	if _, ok := devCommands["docker.state"]; ok {
		for _, m := range r.modules {
			if m == "docker" {
				return
			}
		}
		r.modules = append(r.modules, "docker")
	}
}
