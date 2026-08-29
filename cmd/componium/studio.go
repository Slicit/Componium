package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/Slicit/componium/internal/studio"
)

func studioCmd(args []string) error {
	fs := flag.NewFlagSet("studio", flag.ExitOnError)
	scorePath := fs.String("score", "", "score file to edit (required)")
	rigPath := fs.String("rig", "", "rig file, so the room preview knows what is in it")
	mediaPath := fs.String("media", "", "the film, so the timeline can be scrubbed against it")
	addr := fs.String("addr", "127.0.0.1:8722", "address to serve on")
	fs.Parse(args)

	if *scorePath == "" {
		return fmt.Errorf("-score is required")
	}
	s, err := studio.New(*scorePath, *rigPath, *mediaPath)
	if err != nil {
		return err
	}
	fmt.Printf("editing   %s\n", *scorePath)
	fmt.Printf("open      http://%s\n", *addr)
	fmt.Println("ctrl-c to stop")
	return http.ListenAndServe(*addr, s.Handler())
}
