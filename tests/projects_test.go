package tests

import (
	"context"
	"errors"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
	"bridge-gateway/infrastructure"
)

func newProjectStore(t *testing.T) *infrastructure.FileProjectStore {
	t.Helper()
	s, err := infrastructure.NewFileProjectStore(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("new project store: %v", err)
	}
	return s
}

func newKeyStore(t *testing.T) *infrastructure.FileAPIKeyStore {
	t.Helper()
	s, err := infrastructure.NewFileAPIKeyStore(t.TempDir() + "/apikeys.json")
	if err != nil {
		t.Fatalf("new key store: %v", err)
	}
	return s
}

func TestProjectCreateListGetDelete(t *testing.T) {
	uc := application.NewProjectUseCase(newProjectStore(t))
	ctx := context.Background()

	created, err := uc.Create(ctx, " Equipe Data ", "donnees clients")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.Name != "Equipe Data" {
		t.Fatalf("unexpected project: %#v", created)
	}

	projects, err := uc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != created.ID {
		t.Fatalf("expected 1 project, got %#v", projects)
	}

	got, err := uc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "donnees clients" {
		t.Errorf("unexpected description: %q", got.Description)
	}

	if err := uc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := uc.Get(ctx, created.ID); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestProjectCreateRequiresName(t *testing.T) {
	uc := application.NewProjectUseCase(newProjectStore(t))
	_, err := uc.Create(context.Background(), "  ", "")
	if err == nil {
		t.Fatal("expected validation error")
	}
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if !contains(verr.Fields, "name") {
		t.Errorf("expected name field, got %v", verr.Fields)
	}
}

func TestCreateWithIDIsIdempotent(t *testing.T) {
	uc := application.NewProjectUseCase(newProjectStore(t))
	ctx := context.Background()

	if _, err := uc.CreateWithID(ctx, "project-general", "General", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := uc.CreateWithID(ctx, "project-general", "General", ""); err == nil {
		t.Fatal("expected duplicate to fail")
	}
}

func TestAPIKeyCreateReturnsSecretOnce(t *testing.T) {
	projectsStore := newProjectStore(t)
	keyStore := newKeyStore(t)
	projectsUC := application.NewProjectUseCase(projectsStore)
	pid, _ := projectsUC.Create(context.Background(), "Projet", "")

	uc := application.NewAPIKeyUseCase(keyStore, projectsStore)
	created, secret, err := uc.Create(context.Background(), pid.ID, "CI")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Digest == "" || created.Prefix == "" {
		t.Fatalf("expected digest+prefix stored, got %#v", created)
	}
	if secret == created.Digest || secret == created.Prefix {
		t.Fatal("plaintext secret must never equal stored fields")
	}
	if !created.Matches(secret) {
		t.Error("created key should match its own secret")
	}
	if created.Matches("autre-secret") {
		t.Error("key should not match a different secret")
	}
}

func TestAPIKeyCreateValidatesAndScopesToProject(t *testing.T) {
	projectsStore := newProjectStore(t)
	keyStore := newKeyStore(t)
	projectsUC := application.NewProjectUseCase(projectsStore)
	pid, _ := projectsUC.Create(context.Background(), "A", "")
	uc := application.NewAPIKeyUseCase(keyStore, projectsStore)
	ctx := context.Background()

	_, _, err := uc.Create(ctx, pid.ID, "   ") // name manquant
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected validation error, got %v", err)
	}

	_, _, err = uc.Create(ctx, "proj-inconnu", "CI") // projet inexistant
	if !errors.Is(err, domain.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}

	k1, _, err := uc.Create(ctx, pid.ID, "CI")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	keys, err := uc.ListByProject(ctx, pid.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != k1.ID {
		t.Fatalf("expected 1 key, got %#v", keys)
	}

	other, _ := projectsUC.Create(ctx, "B", "")
	if keysOfOther, _ := uc.ListByProject(ctx, other.ID); len(keysOfOther) != 0 {
		t.Fatalf("expected no keys for other project, got %#v", keysOfOther)
	}

	if err := uc.Delete(ctx, k1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if remaining, _ := uc.ListByProject(ctx, pid.ID); len(remaining) != 0 {
		t.Fatalf("expected no keys after delete, got %#v", remaining)
	}
}

func TestAuthUseCaseMasterAndProjectKeys(t *testing.T) {
	projectsStore := newProjectStore(t)
	keyStore := newKeyStore(t)
	projectsUC := application.NewProjectUseCase(projectsStore)
	pid, _ := projectsUC.Create(context.Background(), "Projet", "")
	ctx := context.Background()

	auth := application.NewAuthUseCase("master-secret", keyStore, projectsStore)

	_, err := auth.Authenticate(ctx, "")
	if !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey for empty, got %v", err)
	}
	_, err = auth.Authenticate(ctx, "Bearer mauvais")
	if !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey for bad key, got %v", err)
	}
	_, err = auth.Authenticate(ctx, "Bearer nope")
	if !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}

	master, err := auth.Authenticate(ctx, "Bearer master-secret")
	if err != nil {
		t.Fatalf("master auth: %v", err)
	}
	if master.Scope != "master" || master.Project != nil {
		t.Fatalf("unexpected master result: %#v", master)
	}

	keyUC := application.NewAPIKeyUseCase(keyStore, projectsStore)
	seed, secret, err := keyUC.Create(ctx, pid.ID, "CI")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	res, err := auth.Authenticate(ctx, "Bearer "+secret)
	if err != nil {
		t.Fatalf("project auth: %v", err)
	}
	if res.Scope != "project" || res.Project == nil || res.Project.ID != pid.ID || res.APIKeyID != seed.ID {
		t.Fatalf("unexpected project result: %#v", res)
	}

	if err := keyUC.Delete(ctx, seed.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := auth.Authenticate(ctx, "Bearer "+secret); !errors.Is(err, domain.ErrInvalidAPIKey) {
		t.Fatalf("expected revoked key to fail, got %v", err)
	}
}

func TestConfigActiveIsScopedPerProject(t *testing.T) {
	ctx := context.Background()

	store := NewTestStore(t)
	projectsStore := newProjectStore(t)
	projectsUC := application.NewProjectUseCase(projectsStore)
	pidA, _ := projectsUC.Create(ctx, "A", "")
	pidB, _ := projectsUC.Create(ctx, "B", "")

	uc := application.NewConfigUseCase(store)
	a1, _ := uc.Create(ctx, &domain.ChatConfig{Name: "A1", ProjectID: pidA.ID, Provider: "ollama", Model: "m", CostCenter: "CC"})
	b1, _ := uc.Create(ctx, &domain.ChatConfig{Name: "B1", ProjectID: pidB.ID, Provider: "ollama", Model: "m", CostCenter: "CC"})
	b2, _ := uc.Create(ctx, &domain.ChatConfig{Name: "B2", ProjectID: pidB.ID, Provider: "openai", Model: "m2", CostCenter: "CC"})

	if _, err := uc.SetActive(ctx, pidA.ID, a1.ID); err != nil {
		t.Fatalf("activate A1: %v", err)
	}
	if _, err := uc.SetActive(ctx, pidB.ID, b2.ID); err != nil {
		t.Fatalf("activate B2: %v", err)
	}

	activeA, err := uc.Active(ctx, pidA.ID)
	if err != nil {
		t.Fatalf("active A: %v", err)
	}
	if activeA.ID != a1.ID {
		t.Errorf("expected A1 active in project A, got %s", activeA.ID)
	}

	activeB, err := uc.Active(ctx, pidB.ID)
	if err != nil {
		t.Fatalf("active B: %v", err)
	}
	if activeB.ID != b2.ID {
		t.Errorf("expected B2 active in project B, got %s", activeB.ID)
	}

	// Inactiver A1 ne doit pas toucher a B2.
	if err := uc.Delete(ctx, a1.ID); err != nil {
		t.Fatalf("delete A1: %v", err)
	}
	if _, err := uc.Active(ctx, pidA.ID); !errors.Is(err, domain.ErrNoActiveConfig) {
		t.Fatalf("expected ErrNoActiveConfig for A, got %v", err)
	}
	if activeB, _ := uc.Active(ctx, pidB.ID); activeB.ID != b2.ID {
		t.Errorf("expected B2 still active, got %s", activeB.ID)
	}
	_ = b1
}
