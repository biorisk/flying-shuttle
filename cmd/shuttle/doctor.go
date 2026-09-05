package main

import (
	"fmt"
	"log"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/project"
)

// runDoctor implements `shuttle doctor` / `shuttle doctor --fix`: it checks
// every evidence row in the current project against its bound corpus.
func runDoctor(args []string) error {
	fix, verbose := false, false
	for _, a := range args {
		switch a {
		case "--fix":
			fix = true
		case "-v", "--verbose":
			verbose = true
		default:
			return fmt.Errorf("usage: shuttle doctor [--fix] [--verbose]")
		}
	}

	bind, err := project.Resolve()
	if err != nil {
		return err
	}
	d, err := doc.Open(bind.Project.DB)
	if err != nil {
		return err
	}
	defer d.Close()

	if bind.Corpus == nil {
		fmt.Printf("project %q is unbound (no corpus) — evidence cannot be checked.\n", bind.Project.Name)
		return nil
	}
	c, err := corpus.Open(bind.Corpus.DB, true)
	if err != nil {
		return err
	}
	defer c.Close()

	evs, err := d.ListAllEvidence()
	if err != nil {
		return err
	}

	var unresolved, superseded, drift, edited []model.Evidence
	for _, e := range evs {
		content, del, found, err := c.ChunkContentAnyState(e.ChunkID)
		if err != nil {
			return err
		}
		switch {
		case !found:
			unresolved = append(unresolved, e)
		case del:
			superseded = append(superseded, e)
		case excerptDiverges(e, content) && e.Edited:
			edited = append(edited, e)
		case excerptDiverges(e, content):
			drift = append(drift, e)
		}
	}

	fmt.Printf("project %q  ⇄  corpus %q\n", bind.Project.Name, bind.Corpus.Name)
	fmt.Printf("  %d evidence rows\n", len(evs))
	report("chunk id unresolved", unresolved, true)
	report("cites a superseded (re-ingested) chunk", superseded, true)
	report("excerpt diverges from source, NOT marked edited (possible drift)", drift, true)
	report("excerpt edited in this project (expected)", edited, verbose)

	if len(unresolved)+len(superseded)+len(drift) == 0 {
		fmt.Println("  ✓ all citations resolve")
		return nil
	}
	if !fix {
		fmt.Println("\nrun `shuttle doctor --fix` to detach the unresolved rows.")
		return nil
	}
	n := 0
	for _, e := range unresolved {
		if err := d.DeleteEvidence(e.ID); err != nil {
			return err
		}
		_ = d.DeleteNode(e.NodeID) // the chunk_ref bullet has no meaning without its evidence
		n++
	}
	fmt.Printf("\ndetached %d dangling evidence row(s). Superseded/drift rows left for review.\n", n)
	return nil
}

// excerptDiverges reports whether the stored excerpt no longer matches the
// corresponding slice of its source chunk.
func excerptDiverges(e model.Evidence, chunkContent string) bool {
	r := []rune(chunkContent)
	a, b := e.CharStart, e.CharEnd
	if a < 0 || b > len(r) || a > b {
		return e.Text != chunkContent
	}
	return string(r[a:b]) != e.Text
}

func report(label string, rows []model.Evidence, show bool) {
	if len(rows) == 0 {
		return
	}
	fmt.Printf("  • %d %s\n", len(rows), label)
	if !show {
		return
	}
	for _, e := range rows {
		txt := e.Text
		if len(txt) > 60 {
			txt = txt[:57] + "…"
		}
		log.Printf("      node %s  chunk %s  %q", e.NodeID, e.ChunkID, txt)
	}
}
