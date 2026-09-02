package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Slicit/componium/internal/store"
	"github.com/Slicit/componium/internal/store/pg"
	"github.com/Slicit/componium/internal/studio"
)

// Moving observations already on disk into the database.
//
// A one shot, and safe to run twice: every row is keyed on the film and the
// moment, so a second run replaces what the first wrote rather than doubling
// it. That property is not incidental. This is the command somebody reaches for
// when they are not sure whether it worked the first time.
//
// It leaves the files where they are. An import that deletes its source is an
// import nobody runs on data they care about, and these took a GPU and a decode
// to produce.

func importVisionCmd(args []string) error {
	fs := flag.NewFlagSet("import-vision", flag.ExitOnError)
	scores := fs.String("scores", "", "directory of scores and their .seen.jsonl files (required)")
	db := fs.String("db", os.Getenv("COMPONIUM_DB"), "Postgres URL (required)")
	dry := fs.Bool("dry-run", false, "say what would be imported and import nothing")
	fs.Parse(args)

	if *scores == "" || *db == "" {
		return fmt.Errorf("both -scores and -db are required")
	}

	entries, err := os.ReadDir(*scores)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".seen.jsonl") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		fmt.Printf("no .seen.jsonl files in %s\n", *scores)
		return nil
	}

	ctx := context.Background()
	var st *pg.Store
	if !*dry {
		st, err = pg.Open(ctx, *db)
		if err != nil {
			return err
		}
		defer st.Close()
	}

	total := 0
	for _, name := range files {
		// `sintel.componium.seen.jsonl` names the film `sintel`, the same way
		// the studio does. See studio.FilmKey.
		film := studio.FilmKey(strings.TrimSuffix(name, ".seen.jsonl"))

		body, err := os.ReadFile(filepath.Join(*scores, name))
		if err != nil {
			return err
		}
		obs, skipped := readSeenFile(string(body), film)
		note := ""
		if skipped > 0 {
			// Ordinary rather than alarming: the file is written a line at a
			// time by something that can be interrupted.
			note = fmt.Sprintf(", %d unreadable lines skipped", skipped)
		}
		fmt.Printf("%-52s %5d observations%s\n", film, len(obs), note)
		total += len(obs)

		if *dry || len(obs) == 0 {
			continue
		}
		if err := st.SaveObservations(ctx, obs); err != nil {
			return fmt.Errorf("%s: %w", film, err)
		}
	}

	if *dry {
		fmt.Printf("\n%d observations in %d films, and nothing written\n", total, len(files))
		return nil
	}
	fmt.Printf("\n%d observations in %d films\n", total, len(files))
	fmt.Println("the files are left where they are; nothing here deletes them")
	return nil
}

// readSeenFile parses a joined observation file, counting what it could not.
func readSeenFile(body, film string) ([]store.Observation, int) {
	var out []store.Observation
	skipped := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var o studio.Observation
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			skipped++
			continue
		}
		out = append(out, store.Observation{
			Film: film, At: o.T, Place: o.Place, Doing: o.Doing,
			Seen: o.Seen, Labels: o.Labels,
		})
	}
	return out, skipped
}
