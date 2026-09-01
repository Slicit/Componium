package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Slicit/componium/internal/studio"
)

func studioCmd(args []string) error {
	fs := flag.NewFlagSet("studio", flag.ExitOnError)
	scorePath := fs.String("score", "", "score to open; with -media a directory, defaults to the selected film's score")
	rigPath := fs.String("rig", "", "rig file, so the room preview knows what is in it")
	mediaPath := fs.String("media", "", "a film, or a directory of them")
	scoresPath := fs.String("scores", "", "where generated scores live (default: beside the films)")
	composer := fs.String("composer", "", "path to compose.py, so the library can analyse films")
	firmware := fs.String("firmware", "", "directory of node firmware images the admin page can flash")
	addr := fs.String("addr", "127.0.0.1:8722", "address to serve on")
	fs.Parse(args)

	if *scorePath == "" && *mediaPath == "" {
		return fmt.Errorf("give -score, or -media pointing at a directory of films")
	}

	// Find the composer without being told, when it is sitting where it
	// usually sits. Requiring the flag for the common case would mean most
	// people never discover the library can build anything.
	comp := *composer
	if comp == "" {
		for _, guess := range []string{
			"composer/compose.py",
			"/opt/componium/composer/compose.py",
			filepath.Join(filepath.Dir(os.Args[0]), "composer", "compose.py"),
		} {
			if _, err := os.Stat(guess); err == nil {
				comp = guess
				break
			}
		}
	}

	s, err := studio.New(studio.Options{
		Score: *scorePath, Rig: *rigPath, Media: *mediaPath,
		Scores: *scoresPath, Composer: comp, Firmware: *firmware,
	})
	if err != nil {
		return err
	}

	fmt.Printf("open      http://%s\n", *addr)
	if comp != "" {
		fmt.Printf("composer  %s\n", comp)
	} else {
		fmt.Println("composer  not found, so the library cannot analyse films")
	}
	fmt.Println("ctrl-c to stop")
	return http.ListenAndServe(*addr, s.Handler())
}
