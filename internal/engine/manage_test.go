package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackmo18/realm-ai/internal/bot"
	"github.com/blackmo18/realm-ai/internal/store"
)

func strp(s string) *string     { return &s }
func slp(s ...string) *[]string { return &s }

func TestCreateUpdateAndStatus(t *testing.T) {
	f := setup(t)
	v, err := f.e.CreateBot(BotInput{Scope: "app", Name: "tester", Charter: "Writes tests.", Model: "haiku"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Handle != "@app/tester" || strings.Join(v.Tools, ",") != "Read,Grep,Glob" || v.Topics == nil {
		t.Fatalf("create: %+v", v)
	}
	none, err := f.e.CreateBot(BotInput{Scope: "app", Name: "quiet", Charter: "x", Tools: []string{}})
	if err != nil || len(none.Tools) != 0 {
		t.Fatalf("explicit empty tools: %+v %v", none, err)
	}
	if _, err := f.e.CreateBot(BotInput{Scope: "app", Name: "bad", Charter: "x", Provider: "nope"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, err := f.e.CreateBot(BotInput{Scope: "nope", Name: "x", Charter: "x"}); err == nil {
		t.Fatal("unknown scope accepted")
	}
	u, err := f.e.UpdateBot("@app/tester", BotPatch{Model: strp("opus"), Effort: strp("high"), Topics: slp("tests"), Tools: slp("Read"), Skills: slp("*"), Charter: strp("Writes and fixes tests."), Provider: strp("claude-cli")})
	if err != nil || u.Model != "opus" || u.Effort != "high" || u.Tools[0] != "Read" || u.Skills[0] != "*" {
		t.Fatalf("update: %+v %v", u, err)
	}
	if _, err := f.e.UpdateBot("@app/tester", BotPatch{Effort: strp("ludicrous")}); err == nil {
		t.Fatal("invalid effort accepted")
	}
	if _, err := f.e.UpdateBot("@app/tester", BotPatch{Provider: strp("nope")}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, err := f.e.UpdateBot("@ghost", BotPatch{}); err == nil {
		t.Fatal("unknown bot accepted")
	}
	off, n, err := f.e.SetBotStatus("@app/tester", bot.ActionOff)
	if err != nil || off.Status != "off" || n != 0 {
		t.Fatalf("off: %+v %d %v", off, n, err)
	}
	if _, _, err := f.e.SetBotStatus("@app/tester", bot.ActionRestore); err == nil {
		t.Fatal("restore of non-archived bot accepted")
	}
	if _, _, err := f.e.SetBotStatus("@ghost", bot.ActionOn); err == nil {
		t.Fatal("unknown bot accepted")
	}
	got, err := f.e.GetBot("tester", "app")
	if err != nil || got.Status != "off" {
		t.Fatalf("get: %+v %v", got, err)
	}
	all, err := f.e.ListBots("app")
	if err != nil || len(all) < 4 {
		t.Fatalf("list: %d %v", len(all), err)
	}
}

func TestKnowledgeAndInheritance(t *testing.T) {
	f := setup(t)
	en, err := f.e.AddKnowledge("@global/ops", "Deploys", "Fridays at 15:00")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.UpdateKnowledge("@global/ops", en.ID, "Deploy day", "Fridays at 16:00"); err != nil {
		t.Fatal(err)
	}
	v, err := f.e.CreateBot(BotInput{Scope: "app", Name: "heir", Charter: "x", Inherit: "@global/ops"})
	if err != nil || v.KnowledgeCount != 1 || len(v.Lineage) != 1 || v.Lineage[0].InheritedFrom != "@global/ops" {
		t.Fatalf("create with inherit: %+v %v", v, err)
	}
	entries, err := f.e.Knowledge("@app/heir")
	if err != nil || len(entries) != 1 || entries[0].Origin != "@global/ops" || entries[0].Body != "Fridays at 16:00" {
		t.Fatalf("inherited entries: %+v %v", entries, err)
	}
	if n, err := f.e.InheritKnowledge("@app/reviewer", "@global/ops"); err != nil || n != 1 {
		t.Fatalf("inherit into existing: %d %v", n, err)
	}
	if _, err := f.e.InheritKnowledge("@app/reviewer", "@app/reviewer"); err == nil {
		t.Fatal("self inherit accepted")
	}
	if err := f.e.ForgetKnowledge("@global/ops", en.ID); err != nil {
		t.Fatal(err)
	}
	if entries, _ := f.e.Knowledge("@global/ops"); len(entries) != 0 {
		t.Fatalf("forget: %+v", entries)
	}
	if _, err := f.e.Knowledge("@ghost"); err == nil {
		t.Fatal("unknown bot accepted")
	}
	if _, err := f.e.CreateBot(BotInput{Scope: "app", Name: "orphan", Charter: "x", Inherit: "@ghost"}); err == nil {
		t.Fatal("inherit from unknown bot accepted")
	}
}

func TestChatMembersSkillsProvidersSettings(t *testing.T) {
	f := setup(t)
	c := f.chat(t, "app", "dev", store.RoutingMention, "@global/ops")
	members, err := f.e.ChatMembers(c.ID)
	if err != nil || len(members) != 3 {
		t.Fatalf("members: %+v %v", members, err)
	}
	if _, err := f.e.ChatMembers("app/none"); err == nil {
		t.Fatal("missing chat accepted")
	}
	skill := filepath.Join(f.projDir, ".claude", "skills", "db-review")
	_ = os.MkdirAll(skill, 0o755)
	_ = os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: db-review\ndescription: \"Reviews SQL\"\n---\n"), 0o644)
	skills, err := f.e.ListSkills("app")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range skills {
		found = found || (s.Name == "db-review" && s.Source == "project" && s.Description == "Reviews SQL")
	}
	if !found {
		t.Fatalf("project skill missing: %+v", skills)
	}
	providers := f.e.Providers()
	if providers[0].Name != "claude-cli" || len(providers) < 5 {
		t.Fatalf("providers: %+v", providers)
	}
	for _, p := range providers {
		if p.Name == "ollama" && (!p.Ready || p.Note != "no key needed") {
			t.Fatalf("ollama: %+v", p)
		}
	}
	if s := f.e.Settings(); s.MaxTurns != maxTurnsPerMessage || s.ModeratorModel == "" {
		t.Fatalf("settings: %+v", s)
	}
}

func TestOnboarding(t *testing.T) {
	f := setup(t)
	dir := filepath.Join(filepath.Dir(f.projDir), "shop")
	_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# shop"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules"), 0o644)

	info, err := f.e.InspectFolder(dir)
	if err != nil || !info.Exists || !info.IsGit || !info.HasClaudeMD || info.Registered || info.Slug != "shop" {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	if missing, _ := f.e.InspectFolder(filepath.Join(dir, "nope")); missing.Exists {
		t.Fatal("missing folder reported as existing")
	}
	res, err := f.e.Onboard(OnboardInput{Dir: dir, Slug: "coffee-shop", IgnoreDB: true, Starters: []string{"reviewer", "docs"},
		GlobalMembers: []string{"@global/ops"}, Routing: store.RoutingModerated})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "coffee-shop" || len(res.Bots) != 2 || res.Chat.ID != "coffee-shop/general" {
		t.Fatalf("onboard: %+v", res)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(gi) != "node_modules\n.realm/realm.db*\n" {
		t.Fatalf(".gitignore = %q", gi)
	}
	if _, err := f.e.RegisterProjectAs(dir, "", true); err != nil {
		t.Fatal(err)
	}
	if gi2, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(gi2) != string(gi) {
		t.Fatalf(".gitignore line duplicated: %q", gi2)
	}
	if info, _ := f.e.InspectFolder(dir); !info.Registered || info.Slug != "coffee-shop" {
		t.Fatalf("after onboarding: %+v", info)
	}
	members, _ := f.e.ChatMembers(res.Chat.ID)
	if len(members) != 3 {
		t.Fatalf("members: %+v", members)
	}
	if _, err := f.e.Onboard(OnboardInput{Dir: filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("missing folder accepted")
	}
	other := filepath.Join(filepath.Dir(f.projDir), "other")
	_ = os.MkdirAll(other, 0o755)
	if _, err := f.e.Onboard(OnboardInput{Dir: other, Starters: []string{"wizard"}}); err == nil {
		t.Fatal("unknown starter accepted")
	}
	if len(Templates()) != 3 {
		t.Fatal("templates changed")
	}
	if _, err := ExpandPath(" "); err == nil {
		t.Fatal("empty path accepted")
	}
	if p, _ := ExpandPath("~/x"); !strings.HasSuffix(p, "/x") || strings.HasPrefix(p, "~") {
		t.Fatalf("expand: %s", p)
	}
}

func TestSuggestFolders(t *testing.T) {
	f := setup(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{"code/a/.git", "code/group/b/.git", "code/node_modules/c/.git", "code/deep/x/y/z/.git", "Projects/d/.git"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	if _, err := f.e.RegisterProject(filepath.Join(home, "code", "a")); err != nil {
		t.Fatal(err)
	}
	got, err := f.e.SuggestFolders(10)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, ",")
	for _, want := range []string{"code/group/b", "Projects/d"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, bad := range []string{"code/a,", "node_modules", "deep/x/y/z"} {
		if strings.Contains(joined+",", bad) {
			t.Errorf("unexpected %s in %v", bad, got)
		}
	}
	if one, _ := f.e.SuggestFolders(1); len(one) != 1 {
		t.Fatalf("limit ignored: %v", one)
	}
}
