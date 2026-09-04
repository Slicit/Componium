package studio

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/boards"
	"github.com/Slicit/componium/internal/cip"
)

// The boards this installation knows about, and which of them are switched on.
//
// Attaching a board used to leave no trace: the address was typed, used once,
// and forgotten when the page closed. So every time anybody wanted to look at a
// board they went and found its address again, and there was no list of what
// the installation actually has.
//
// The secret never comes back out. It goes in on a save, is stored in the
// boards file, and from then on the studio uses it on the caller's behalf: a
// board is reached by name, and the browser never has to hold the string that
// authorises reconfiguring it.

// checkTimeout is how long a board has to answer before it is called offline.
//
// Short, because this runs for every board at once and somebody is watching a
// page. A board that needs longer than this to say hello is a board that is not
// going to land a cue on a frame either.
const checkTimeout = 2 * time.Second

type wireBoard struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Note string `json:"note,omitempty"`
	// Secret is write only. It is accepted on a save and never returned; the
	// page shows whether one is held, not what it is.
	Secret    string `json:"secret,omitempty"`
	HasSecret bool   `json:"hasSecret"`
}

type wireBoardStatus struct {
	Name   string `json:"name"`
	Addr   string `json:"addr"`
	Online bool   `json:"online"`
	// Why is what went wrong, for a board that did not answer. Worth carrying:
	// a wrong secret and an unplugged board look identical from here, and the
	// message is the only place that distinction can be drawn.
	Why         string               `json:"why,omitempty"`
	Firmware    string               `json:"firmware,omitempty"`
	Instruments []wireNodeInstrument `json:"instruments,omitempty"`
}

func (s *Server) boardsList() []wireBoard {
	out := []wireBoard{}
	for _, b := range s.boards.All() {
		out = append(out, wireBoard{
			Name: b.Name, Addr: b.Addr, Note: b.Note,
			HasSecret: b.Secret != "",
		})
	}
	return out
}

// handleBoards lists the shelf, or replaces it.
func (s *Server) handleBoards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"editable": s.boards.Editable(),
			"boards":   s.boardsList(),
		})

	case http.MethodPut:
		var want struct {
			Boards []wireBoard `json:"boards"`
		}
		if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
			http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.boards.Editable() {
			http.Error(w, "this studio was started without a boards file, so there is "+
				"nowhere to remember them", http.StatusBadRequest)
			return
		}

		next := make([]boards.Board, 0, len(want.Boards))
		for _, b := range want.Boards {
			secret := b.Secret
			if secret == "" {
				// Kept rather than cleared. The page never receives a secret,
				// so it cannot send one back, and an edit to a note would
				// otherwise silently lock us out of the board.
				if was, ok := s.boards.Find(b.Name); ok {
					secret = was.Secret
				}
			}
			next = append(next, boards.Board{
				Name: b.Name, Addr: b.Addr, Note: b.Note, Secret: secret,
			})
		}
		if err := s.boards.Save(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"editable": true,
			"boards":   s.boardsList(),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBoardsCheck asks every board whether it is there.
//
// All at once, because doing it one after another means a page that takes
// however many boards times the timeout to load, and the whole point is a
// glance.
func (s *Server) handleBoardsCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	list := s.boards.All()
	s.mu.Unlock()

	out := make([]wireBoardStatus, len(list))
	var wg sync.WaitGroup
	for i, b := range list {
		wg.Add(1)
		go func(i int, b boards.Board) {
			defer wg.Done()
			out[i] = checkBoard(b)
		}(i, b)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{"boards": out})
}

func checkBoard(b boards.Board) wireBoardStatus {
	st := wireBoardStatus{Name: b.Name, Addr: b.Addr}

	c, err := cip.Dial(b.Addr, checkTimeout, b.Secret)
	if err != nil {
		st.Why = err.Error()
		if b.Secret == "" {
			st.Why += "; no secret is stored for this board, and a board that has " +
				"one ignores anyone without it"
		}
		return st
	}
	defer c.Close()

	st.Online = true
	node := describe(c)
	st.Firmware = node.Firmware
	st.Instruments = node.Instruments
	return st
}
