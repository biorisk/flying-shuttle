package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/project"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	datastar "github.com/starfederation/datastar-go/datastar"
)

func (h *handlers) projectBarView() viewmodel.ProjectBar {
	vm := viewmodel.ProjectBar{
		Current:   h.d.ProjectName,
		CanSwitch: h.d.SwitchProject != nil && h.d.ProjectHome != "",
	}
	vm.CorpusName = h.d.CorpusName
	if h.d.ProjectHome != "" {
		names, err := project.ListProjects(h.d.ProjectHome)
		if err != nil {
			log.Printf("project bar: list: %v", err)
		}
		vm.Names = names
		if corpora, err := project.ListCorpora(h.d.ProjectHome); err == nil {
			vm.Corpora = corpora
		}
	}
	if len(vm.Names) == 0 && vm.Current != "" {
		vm.Names = []string{vm.Current}
	}
	return vm
}

// bindCorpus writes the current project's corpus binding and restarts the
// server so the new corpus is opened.
//
//	POST /project/bind-corpus?name=<corpus>
func (h *handlers) bindCorpus(w http.ResponseWriter, r *http.Request) {
	if h.d.SwitchProject == nil || h.d.ProjectHome == "" {
		http.Error(w, "corpus binding disabled", http.StatusForbidden)
		return
	}
	name := r.URL.Query().Get("name")
	if !project.ValidName(name) {
		http.Error(w, "invalid corpus name", http.StatusBadRequest)
		return
	}
	if _, err := project.CreateCorpus(h.d.ProjectHome, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pp := project.ProjectPathsFor(h.d.ProjectHome, h.d.ProjectName)
	if err := project.WriteBinding(pp, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Re-exec on the same project; boot re-reads project.json.
	h.triggerRestart(w, r, h.d.ProjectName)
}

// projectSwitch persists the target project and triggers the server restart;
// the client shows a "switching…" state and reloads once the server is back.
//
//	POST /project/switch?name=<project>
func (h *handlers) projectSwitch(w http.ResponseWriter, r *http.Request) {
	h.doSwitch(w, r, r.URL.Query().Get("name"), false)
}

// projectNew creates a project directory, then switches to it.
//
//	POST /project/new   (form: name)
func (h *handlers) projectNew(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	h.doSwitch(w, r, r.FormValue("name"), true)
}

func (h *handlers) doSwitch(w http.ResponseWriter, r *http.Request, name string, create bool) {
	if h.d.SwitchProject == nil || h.d.ProjectHome == "" {
		http.Error(w, "project switching disabled", http.StatusForbidden)
		return
	}
	if !project.ValidName(name) {
		http.Error(w, "invalid project name", http.StatusBadRequest)
		return
	}
	if name == h.d.ProjectName && !create {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if create {
		if _, err := project.CreateProject(h.d.ProjectHome, name, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	h.triggerRestart(w, r, name)
}

// triggerRestart streams the "switching…" bar, schedules a poll-and-reload on
// the client, and fires the server re-exec.
func (h *handlers) triggerRestart(w http.ResponseWriter, r *http.Request, name string) {
	sse := datastar.NewSSE(w, r)
	vm := h.projectBarView()
	vm.Switching = name
	_ = sse.PatchElementTempl(components.ProjectBar(vm))
	// The re-exec drops this connection. Wait past the graceful-shutdown
	// window, then poll /healthz and reload once the new server answers.
	_ = sse.ExecuteScript(
		"setTimeout(function poll(){" +
			"fetch('/healthz',{cache:'no-store'})" +
			".then(function(r){return r.ok?location.reload():setTimeout(poll,500)})" +
			".catch(function(){setTimeout(poll,500)})" +
			"},1200)",
	)

	// Fire the restart after the response has flushed.
	go h.d.SwitchProject(name)
}
