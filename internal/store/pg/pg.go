package pg

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Slicit/componium/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ConnectTimeout bounds the first hello. Generous against a container that has
// just started and mean against one that will never answer.
const ConnectTimeout = 10 * time.Second

// QueryTimeout bounds an ordinary read or write.
//
// Nothing here is a long query: the largest is a film's observations, a few
// thousand rows on a primary key. A query that takes longer than this is not
// slow, it is stuck, and the caller should be told so rather than left holding
// a request open until a browser gives up first.
const QueryTimeout = 30 * time.Second

// bounded returns a context that will not wait for ever.
//
// Applied inside each method rather than asked of every caller. The callers are
// HTTP handlers and a merge step, and a rule that every one of them must
// remember a deadline is a rule that will be forgotten exactly once.
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, QueryTimeout)
}

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
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	// Small on purpose. One studio serving one browser and one analysis is not
	// a workload; the pool exists so that a slow query does not block the next
	// request, not to saturate anything.
	cfg.MaxConns = 8
	cfg.MinConns = 1
	// Recycled, so a connection that has been idle through a database restart
	// or a network hiccup is replaced rather than discovered at the worst
	// moment by whoever asked next.
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	// Bounded, because the failure this catches is a database that accepts a
	// connection and never answers. Without a deadline that is a studio which
	// never finishes starting, which looks exactly like a studio that crashed.
	reach, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()
	if err := pool.Ping(reach); err != nil {
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

// SaveObservations writes a run's observations in one transaction.
//
// COPY into a temp table and a single insert-select from it, rather than an
// upsert per row. Two reasons, and neither is micro-optimisation.
//
// It is atomic. A batch is a batch and not a transaction, so a connection lost
// halfway through a feature used to leave half a film's observations in the
// database, with nothing from the outside able to tell that from a film the
// model had less to say about. Either the whole run lands or none of it does.
//
// And COPY is the fastest ingest Postgres has. The library's 8,746 observations
// went in as 8,746 statements in about 445ms; as three COPYs and three inserts
// it is a fraction of that, and the difference is per analysis rather than once.
func (s *Store) SaveObservations(ctx context.Context, obs []store.Observation) error {
	if len(obs) == 0 {
		return nil
	}
	ctx, cancel := bounded(ctx)
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: saving observations: %w", err)
	}
	defer tx.Rollback(ctx)

	// Dropped with the transaction, so nothing survives a failure and two
	// analyses running at once cannot see each other's rows.
	if _, err := tx.Exec(ctx, `
		create temp table incoming (like observation including defaults)
		on commit drop`); err != nil {
		return fmt.Errorf("store: saving observations: %w", err)
	}

	// Deduplicated here rather than in SQL. A single run can describe the same
	// moment twice and COPY carries both; ON CONFLICT cannot update a row it
	// inserted in the same statement, so the alternative was a distinct on with
	// an order by, which sorts every row on the way in. A map is cheaper, and
	// it is what the in-memory store does, so both reach the same answer by the
	// same rule: the last one wins.
	type moment struct {
		film string
		at   float64
	}
	latest := make(map[moment]int, len(obs))
	order := make([]moment, 0, len(obs))
	for i, o := range obs {
		m := moment{o.Film, o.At}
		if _, dup := latest[m]; !dup {
			order = append(order, m)
		}
		latest[m] = i
	}

	rows := make([][]any, 0, len(order))
	for _, m := range order {
		o := obs[latest[m]]
		// A nil slice is SQL NULL, and the column is NOT NULL because an
		// observation with no labels has no labels rather than an unknown
		// number of them. Caught by the contract suite on its first run
		// against a real database, which is the entire argument for having
		// one: the in-memory store was perfectly happy.
		labels := o.Labels
		if labels == nil {
			labels = []string{}
		}
		rows = append(rows, []any{o.Film, o.At, o.Place, o.Doing, o.Seen, labels})
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"incoming"},
		[]string{"film", "at", "place", "doing", "seen", "labels"},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("store: saving observations: %w", err)
	}

	// The conflict clause is the fix for a bug this project has already had:
	// analysis is resumed, retried and re-run, and observations that stacked
	// instead of replacing turned 459 distinct moments into 3720 rows and
	// needed a repair script. Here it is one line of SQL.
	//
	if _, err := tx.Exec(ctx, `
		insert into observation (film, at, place, doing, seen, labels)
		select film, at, place, doing, seen, labels from incoming
		on conflict (film, at) do update set
			place = excluded.place, doing = excluded.doing,
			seen = excluded.seen, labels = excluded.labels,
			built_at = now()`); err != nil {
		return fmt.Errorf("store: saving observations: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) Observations(ctx context.Context, film string) ([]store.Observation, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
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

func (s *Store) HasObservations(ctx context.Context, film string) (bool, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
	var any bool
	err := s.pool.QueryRow(ctx,
		`select exists (select 1 from observation where film = $1)`, film).Scan(&any)
	return any, err
}

func (s *Store) ForgetObservations(ctx context.Context, film string) error {
	ctx, cancel := bounded(ctx)
	defer cancel()
	_, err := s.pool.Exec(ctx, `delete from observation where film = $1`, film)
	return err
}

func (s *Store) Films(ctx context.Context) ([]string, error) {
	ctx, cancel := bounded(ctx)
	defer cancel()
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
