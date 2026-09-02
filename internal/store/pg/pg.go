package pg

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Slicit/componium/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the real one.
//
// A pool rather than a connection, because the studio serves HTTP and the
// composer runs a thread pool, and both will ask at once. pgx needs no cgo,
// which is worth saying out loud: it is the reason this is a better fit than
// SQLite for a project that cross compiles a static binary to a Pi, and it
// inverts the argument SQLite is usually defended with.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects and brings the schema up to date.
//
// Migrating on open rather than in a separate command is a choice for a
// project somebody runs at home: a studio that starts and works is worth more
// than a studio that starts and tells you to run a migration first.
func Open(ctx context.Context, url string) (*Store, error) {
	if strings.TrimSpace(url) == "" {
		return nil, store.ErrNoStore
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: cannot reach the database: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies every migration that has not been applied, in order.
//
// Numbered SQL files and eighty lines, rather than a framework. Every
// migration library in this space arrives with opinions about a schema this
// project is perfectly capable of writing down in SQL, and the ones that do
// not are still a dependency that has to be kept current for the lifetime of
// the database.
//
// Forward only. A down migration is a thing people write once, never test, and
// reach for at the worst possible moment.
func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		create table if not exists schema_migration (
			name       text primary key,
			applied_at timestamptz not null default now()
		)`)
	if err != nil {
		return fmt.Errorf("store: migration table: %w", err)
	}

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	// By name, which is why they are numbered.
	sort.Strings(names)

	for _, name := range names {
		var done bool
		err := s.pool.QueryRow(ctx,
			`select exists (select 1 from schema_migration where name = $1)`, name).Scan(&done)
		if err != nil {
			return fmt.Errorf("store: %s: %w", name, err)
		}
		if done {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		// One transaction per migration, so a failure leaves the database on
		// the last version that worked rather than halfway into this one.
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`insert into schema_migration (name) values ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) SaveObservations(ctx context.Context, obs []store.Observation) error {
	if len(obs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, o := range obs {
		// A nil slice is SQL NULL, and the column is NOT NULL because an
		// observation with no labels has no labels rather than an unknown
		// number of them. Caught by the contract suite on its first run
		// against a real database, which is the entire argument for having
		// one: the in-memory store was perfectly happy.
		labels := o.Labels
		if labels == nil {
			labels = []string{}
		}
		// The conflict clause is the fix for a bug this project has already
		// had: analysis is resumed, retried and re-run, and observations that
		// stacked instead of replacing turned 459 distinct moments into 3720
		// rows and needed a repair script. Here it is one line of SQL.
		batch.Queue(`
			insert into observation (film, at, place, doing, seen, labels)
			values ($1, $2, $3, $4, $5, $6)
			on conflict (film, at) do update set
				place = excluded.place, doing = excluded.doing,
				seen = excluded.seen, labels = excluded.labels,
				built_at = now()`,
			o.Film, o.At, o.Place, o.Doing, o.Seen, labels)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range obs {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("store: saving observations: %w", err)
		}
	}
	return nil
}

func (s *Store) Observations(ctx context.Context, film string) ([]store.Observation, error) {
	rows, err := s.pool.Query(ctx, `
		select film, at, place, doing, seen, labels
		from observation where film = $1 order by at`, film)
	if err != nil {
		return nil, fmt.Errorf("store: reading observations: %w", err)
	}
	defer rows.Close()

	out := []store.Observation{}
	for rows.Next() {
		var o store.Observation
		if err := rows.Scan(&o.Film, &o.At, &o.Place, &o.Doing, &o.Seen, &o.Labels); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) ForgetObservations(ctx context.Context, film string) error {
	_, err := s.pool.Exec(ctx, `delete from observation where film = $1`, film)
	return err
}

func (s *Store) Films(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`select distinct film from observation order by film`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var film string
		if err := rows.Scan(&film); err != nil {
			return nil, err
		}
		out = append(out, film)
	}
	return out, rows.Err()
}

func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

var _ store.Store = (*Store)(nil)
