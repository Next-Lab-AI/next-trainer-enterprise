package pools

import (
	"context"
	"errors"
	"testing"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
)

func setup(t *testing.T) (store.Store, *Service, *store.User, *store.User, *store.Pool) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	admin := &store.User{ID: store.NewID(), Username: "admin", PasswordHash: "h", Role: store.RoleAdmin, CreatedAt: 1, UpdatedAt: 1}
	user := &store.User{ID: store.NewID(), Username: "u1", PasswordHash: "h", Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	db.Users().Create(ctx, admin)
	db.Users().Create(ctx, user)
	pool := &store.Pool{PoolID: store.NewID(), Name: "p1", CreatedAt: 1}
	db.Pools().Create(ctx, pool)
	return db, New(db), admin, user, pool
}

func TestCanSubmit(t *testing.T) {
	db, svc, admin, user, pool := setup(t)
	ctx := context.Background()

	ok, _ := svc.CanSubmit(ctx, admin, pool.PoolID)
	if !ok {
		t.Fatal("admin always allowed")
	}
	ok, _ = svc.CanSubmit(ctx, user, pool.PoolID)
	if ok {
		t.Fatal("user without grant")
	}
	if err := db.Pools().GrantUser(ctx, user.ID, pool.PoolID); err != nil {
		t.Fatal(err)
	}
	ok, _ = svc.CanSubmit(ctx, user, pool.PoolID)
	if !ok {
		t.Fatal("user with grant")
	}
}

func TestCanUseDataset(t *testing.T) {
	db, svc, _, _, pool := setup(t)
	ctx := context.Background()

	ok, _ := svc.CanUseDataset(ctx, "ds1", pool.PoolID)
	if ok {
		t.Fatal("ungranted dataset")
	}
	db.Pools().GrantDataset(ctx, "ds1", pool.PoolID)
	ok, _ = svc.CanUseDataset(ctx, "ds1", pool.PoolID)
	if !ok {
		t.Fatal("granted dataset")
	}
}

func TestCheckSubmit(t *testing.T) {
	db, svc, _, user, pool := setup(t)
	ctx := context.Background()
	db.Pools().GrantUser(ctx, user.ID, pool.PoolID)

	if err := svc.CheckSubmit(ctx, user, pool.PoolID, []string{"ds1"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("dataset not granted: %v", err)
	}
	db.Pools().GrantDataset(ctx, "ds1", pool.PoolID)
	if err := svc.CheckSubmit(ctx, user, pool.PoolID, []string{"ds1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckSubmit(ctx, user, store.NewID(), nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("pool not granted: %v", err)
	}
}

func TestVisiblePools(t *testing.T) {
	db, svc, admin, user, pool := setup(t)
	ctx := context.Background()
	db.Pools().Create(ctx, &store.Pool{PoolID: store.NewID(), Name: "p2", CreatedAt: 2})
	db.Pools().GrantUser(ctx, user.ID, pool.PoolID)

	adminPools, _ := svc.VisiblePools(ctx, admin)
	if len(adminPools) != 2 {
		t.Fatalf("admin sees %d", len(adminPools))
	}
	userPools, _ := svc.VisiblePools(ctx, user)
	if len(userPools) != 1 || userPools[0].PoolID != pool.PoolID {
		t.Fatalf("user sees %v", userPools)
	}
}
