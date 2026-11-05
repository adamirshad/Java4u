package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// ------------------------------------------------------------ rules / args

func rule(action, os string) Rule {
	r := Rule{Action: action}
	if os != "" {
		r.OS = &OSRule{Name: os}
	}
	return r
}

func TestEmptyRulesAllow(t *testing.T) {
	if !RulesAllow(nil, Features{}) {
		t.Fatal("empty rules should allow")
	}
}

func TestLastMatchingRuleWins(t *testing.T) {
	if RulesAllow([]Rule{rule("allow", ""), rule("disallow", CurrentOS())}, Features{}) {
		t.Fatal("last matching rule should win")
	}
}

func TestNonMatchingOSRuleIsDisallowed(t *testing.T) {
	other := "windows"
	if CurrentOS() == "windows" {
		other = "linux"
	}
	if RulesAllow([]Rule{rule("allow", other)}, Features{}) {
		t.Fatal("rule for another OS should not allow")
	}
}

func TestFeatureRulesDefaultToFalse(t *testing.T) {
	r := Rule{Action: "allow", Features: map[string]bool{"is_demo_user": true}}
	if RulesAllow([]Rule{r}, Features{}) {
		t.Fatal("missing feature should count as false")
	}
	if !RulesAllow([]Rule{r}, Features{"is_demo_user": true}) {
		t.Fatal("feature set should allow")
	}
}

func TestMavenPaths(t *testing.T) {
	p, ok := mavenPath("net.sf.jopt-simple:jopt-simple:5.0.4")
	if !ok || p != "net/sf/jopt-simple/jopt-simple/5.0.4/jopt-simple-5.0.4.jar" {
		t.Fatalf("got %q", p)
	}
	p, _ = mavenPath("org.lwjgl:lwjgl:3.3.1:natives-windows")
	if p != "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-windows.jar" {
		t.Fatalf("classifier: got %q", p)
	}
	if _, ok := mavenPath("bad"); ok {
		t.Fatal("bad name should fail")
	}
}

func TestParsesMixedArguments(t *testing.T) {
	js := `{"game":["--username",{"rules":[{"action":"allow","features":{"is_demo_user":true}}],"value":"--demo"},{"rules":[],"value":["a","b"]}],"jvm":[]}`
	var args Arguments
	if err := json.Unmarshal([]byte(js), &args); err != nil {
		t.Fatal(err)
	}
	got := ExpandArgs(args.Game, Features{})
	if !reflect.DeepEqual(got, []string{"--username", "a", "b"}) {
		t.Fatalf("got %v", got)
	}
}

func TestResolveLibraries(t *testing.T) {
	js := `{"id":"x","libraries":[
	  {"name":"a:b:1","downloads":{"artifact":{"path":"a/b/1/b-1.jar","sha1":"aa","size":3,"url":"https://x/a"}}},
	  {"name":"a:b:1","downloads":{"artifact":{"path":"a/b/1/b-1.jar","sha1":"aa","size":3,"url":"https://x/a"}}},
	  {"name":"old.group:thing:2"},
	  {"name":"skip:me:1","rules":[{"action":"allow","os":{"name":"nonexistent-os"}}],"downloads":{"artifact":{"path":"s.jar","sha1":"","size":1,"url":"u"}}},
	  {"name":"nat:ive:1","natives":{"` + CurrentOS() + `":"natives-${arch}"},"extract":{"exclude":["META-INF/"]},
	   "downloads":{"classifiers":{"natives-` + archBits() + `":{"path":"n.jar","sha1":"bb","size":5,"url":"https://x/n"}}}}
	]}`
	var v VersionJSON
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatal(err)
	}
	r := ResolveLibraries(&v, "libs", Features{})
	if len(r.Classpath) != 2 {
		t.Fatalf("classpath %v", r.Classpath)
	}
	if r.Classpath[1] != filepath.Join("libs", "old", "group", "thing", "2", "thing-2.jar") {
		t.Fatalf("maven path %v", r.Classpath[1])
	}
	if r.Downloads[2].URL != LibrariesBase+"old/group/thing/2/thing-2.jar" || r.Downloads[2].Size != UnknownSize {
		t.Fatalf("maven download %+v", r.Downloads[2])
	}
	if len(r.Natives) != 1 || r.Natives[0].Exclude[0] != "META-INF/" {
		t.Fatalf("natives %+v", r.Natives)
	}
}

func TestSafeRel(t *testing.T) {
	for _, ok := range []string{"bin/java", "a/b.txt", "./x"} {
		if !isSafeRel(ok) {
			t.Errorf("%q should be safe", ok)
		}
	}
	for _, bad := range []string{"../evil", "a/../../evil", "/etc/passwd", `C:\x`, `..\x`, ""} {
		if isSafeRel(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestExtractNatives(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "n.jar")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"lib.dll", "META-INF/MANIFEST.MF", "../escape.txt", "sub/x.so"} {
		w, _ := zw.Create(name)
		w.Write([]byte("hi"))
	}
	zw.Close()
	os.WriteFile(jar, buf.Bytes(), 0o644)

	out := filepath.Join(dir, "out")
	if err := ExtractNatives([]NativeJar{{Path: jar, Exclude: []string{"META-INF/"}}}, out); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(out, "lib.dll")) || !exists(filepath.Join(out, "sub", "x.so")) {
		t.Fatal("expected files missing")
	}
	if exists(filepath.Join(out, "META-INF")) || exists(filepath.Join(dir, "escape.txt")) {
		t.Fatal("excluded or unsafe entry was extracted")
	}
}

// ------------------------------------------------------------ auth / accounts

func TestOfflineUUID(t *testing.T) {
	u := OfflineUUID("Steve")
	if len(u) != 32 || u[12] != '3' {
		t.Fatalf("bad shape %s", u)
	}
	// Known value: UUID.nameUUIDFromBytes("OfflinePlayer:Steve")
	if u != "5627dd98e6be3c21b8a8e92344183641" {
		t.Fatalf("unexpected uuid %s", u)
	}
	if u == OfflineUUID("Alex") {
		t.Fatal("different names should differ")
	}
}

func TestRejectsBadNames(t *testing.T) {
	for _, bad := range []string{"ab", "has space", "waytoolongusername1"} {
		if _, err := OfflineAccount(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if _, err := OfflineAccount("Steve_1"); err != nil {
		t.Error(err)
	}
}

func acct(name string) StoredAccount {
	return StoredAccount{Username: name, UUID: "uuid-" + name, RefreshToken: "r", MCAccessToken: "t"}
}

func TestFindRemoveUpsert(t *testing.T) {
	s := &AccountStore{}
	s.upsert(acct("Alex"))
	s.upsert(acct("Steve"))
	again := acct("Alex")
	again.MCAccessToken = "new"
	s.upsert(again)
	if len(s.Accounts) != 2 {
		t.Fatal("upsert should replace")
	}
	if s.Find("alex").MCAccessToken != "new" {
		t.Fatal("find is case-insensitive and sees the update")
	}
	if s.Find("").Username != "Alex" {
		t.Fatal("empty name finds first")
	}
	if !s.Remove("STEVE") || s.Remove("nobody") {
		t.Fatal("remove")
	}
}

func TestAccountStoreRoundTrip(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	s, err := LoadAccounts(dirs)
	if err != nil || len(s.Accounts) != 0 {
		t.Fatal("missing file should load empty")
	}
	s.ClientID = "cid"
	s.upsert(acct("Alex"))
	if err := s.Save(dirs); err != nil {
		t.Fatal(err)
	}
	back, err := LoadAccounts(dirs)
	if err != nil || back.ClientID != "cid" || back.Find("alex") == nil {
		t.Fatalf("round trip failed: %+v %v", back, err)
	}
	// The Rust launcher's accounts.json format still loads.
	os.WriteFile(dirs.AccountsFile(), []byte(`{"client_id":null,"accounts":[{"username":"A","uuid":"u","refresh_token":"r","mc_access_token":"t","mc_expires_at":5}]}`), 0o600)
	old, err := LoadAccounts(dirs)
	if err != nil || old.Accounts[0].MCExpiresAt != 5 {
		t.Fatalf("legacy format: %+v %v", old, err)
	}
}

// ------------------------------------------------------------ optimize

func TestOffAddsNothing(t *testing.T) {
	if len(JVMFlags(ProfileOff, 4096, nil)) != 0 {
		t.Fatal()
	}
}

func TestUserGCChoiceWins(t *testing.T) {
	if f := JVMFlags(ProfilePerformance, 4096, []string{"-XX:+UseZGC", "-Xms2G"}); len(f) != 0 {
		t.Fatalf("got %v", f)
	}
}

func TestInitialHeapIsCapped(t *testing.T) {
	if f := JVMFlags(ProfileBalanced, 512, nil); f[0] != "-Xms512M" {
		t.Fatal(f[0])
	}
	if f := JVMFlags(ProfileBalanced, 8192, nil); f[0] != "-Xms1024M" {
		t.Fatal(f[0])
	}
}

func TestExperimentalUnlockPrecedesExperimentalFlags(t *testing.T) {
	f := JVMFlags(ProfilePerformance, 4096, nil)
	unlock := slices.Index(f, "-XX:+UnlockExperimentalVMOptions")
	g1 := slices.IndexFunc(f, func(s string) bool { return strings.HasPrefix(s, "-XX:G1NewSizePercent") })
	if unlock < 0 || g1 < 0 || unlock > g1 {
		t.Fatal("unlock must come first")
	}
}

func TestRecommendedMemoryIsSane(t *testing.T) {
	if m := RecommendedMemoryMB(); m < 1024 || m > 6144 {
		t.Fatal(m)
	}
	if TotalMemoryMB() <= 0 {
		t.Log("total memory could not be read on this platform")
	}
}

func TestParsesProfiles(t *testing.T) {
	if p, _ := ParseProfile("perf"); p != ProfilePerformance {
		t.Fatal()
	}
	if _, err := ParseProfile("nope"); err == nil {
		t.Fatal()
	}
}

// ------------------------------------------------------------ java

func TestParsesJavaVersions(t *testing.T) {
	cases := map[string]int{
		`openjdk version "21.0.2" 2024-01-16`: 21,
		`java version "1.8.0_312"`:            8,
		`openjdk version "17" 2021`:           17,
		`openjdk version "23-ea"`:             23,
	}
	for in, want := range cases {
		if got, ok := parseMajor(in); !ok || got != want {
			t.Errorf("%s: got %d", in, got)
		}
	}
	if _, ok := parseMajor("nonsense"); ok {
		t.Error("nonsense should fail")
	}
}

// ------------------------------------------------------------ instances

func TestInstances(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	if _, err := CreateInstance(dirs, "../evil", "1.21"); err == nil {
		t.Fatal("bad name accepted")
	}
	if _, err := CreateInstance(dirs, "main", "1.21.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateInstance(dirs, "main", "1.21.1"); err == nil {
		t.Fatal("duplicate accepted")
	}
	CreateInstance(dirs, "alpha", "1.20")
	all, _ := ListInstances(dirs)
	if len(all) != 2 || all[0].Name != "alpha" {
		t.Fatalf("list: %v", all)
	}
	// Defaults fill in for fields missing from older files.
	os.WriteFile(filepath.Join(dirs.InstanceDir("main"), "instance.json"), []byte(`{"name":"main","version":"1.21.1","java_path":null}`), 0o644)
	inst, err := LoadInstance(dirs, "main")
	if err != nil || inst.MemoryMB < 1024 || inst.Profile != ProfileBalanced || inst.JavaPath != "" {
		t.Fatalf("defaults: %+v %v", inst, err)
	}
	if err := DeleteInstance(dirs, "main"); err != nil || exists(dirs.InstanceDir("main")) {
		t.Fatal("delete failed")
	}
}

// ------------------------------------------------------------ wallpaper

func TestWallpaperNames(t *testing.T) {
	if !validWallpaperName("sunset.jpg") {
		t.Fatal()
	}
	for _, bad := range []string{"../x.jpg", "a/b.jpg", ".hidden.jpg", "x.png"} {
		if validWallpaperName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestImportWallpaper(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 5120, 1440)))
	name, err := ImportWallpaper(dirs, "My Pic!.png", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if name != "My_Pic_.jpg" {
		t.Fatalf("name %s", name)
	}
	name2, _ := ImportWallpaper(dirs, "My Pic!.png", bytes.NewReader(buf.Bytes()))
	if name2 != "My_Pic_-1.jpg" {
		t.Fatalf("second name %s", name2)
	}
	p, _ := WallpaperPath(dirs, name)
	f, _ := os.Open(p)
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width != 2560 || cfg.Height != 720 {
		t.Fatalf("resized to %dx%d (%v)", cfg.Width, cfg.Height, err)
	}
	if got := ListWallpapers(dirs); len(got) != 2 {
		t.Fatalf("list %v", got)
	}
	if _, err := ImportWallpaper(dirs, "x.png", strings.NewReader("not an image")); err == nil {
		t.Fatal("garbage accepted")
	}
}

// ------------------------------------------------------------ downloads

func TestDownloadAll(t *testing.T) {
	body := []byte("hello goose")
	sum := sha1.Sum(body)
	good := hex.EncodeToString(sum[:])
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient()
	tasks := []DownloadTask{
		{URL: srv.URL + "/a", Dest: filepath.Join(dir, "a", "f1"), SHA1: good, Size: int64(len(body))},
		{URL: srv.URL + "/b", Dest: filepath.Join(dir, "f2"), Size: UnknownSize},
		{URL: srv.URL + "/a", Dest: filepath.Join(dir, "a", "f1"), SHA1: good, Size: int64(len(body))}, // duplicate
	}
	var last [2]int
	err := DownloadAll(context.Background(), c, tasks, "test", func(_ string, d, n int) { last = [2]int{d, n} }, VerifyHash)
	if err != nil {
		t.Fatal(err)
	}
	if last != [2]int{2, 2} || hits.Load() != 2 {
		t.Fatalf("progress %v hits %d", last, hits.Load())
	}
	// Already valid: no new requests.
	if err := DownloadAll(context.Background(), c, tasks, "test", nil, VerifyHash); err != nil || hits.Load() != 2 {
		t.Fatalf("re-download happened: %v %d", err, hits.Load())
	}
	// Wrong hash fails and leaves nothing behind.
	bad := DownloadTask{URL: srv.URL + "/c", Dest: filepath.Join(dir, "bad"), SHA1: strings.Repeat("0", 40), Size: UnknownSize}
	err = DownloadAll(context.Background(), c, []DownloadTask{bad}, "test", nil, VerifyHash)
	if _, ok := err.(*HashMismatchError); !ok {
		t.Fatalf("want hash mismatch, got %v", err)
	}
	if exists(bad.Dest) || exists(bad.Dest+".part") {
		t.Fatal("bad file left behind")
	}
}

// ------------------------------------------------------------ launch

func TestBuildCommand(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	os.MkdirAll(dirs.AssetIndexesDir(), 0o755)
	os.WriteFile(dirs.AssetIndex("17"), []byte(`{"objects":{}}`), 0o644)
	js := `{"id":"1.21.1","type":"release","mainClass":"net.minecraft.client.main.Main",
	  "assetIndex":{"id":"17"},"downloads":{"client":{}},
	  "arguments":{"jvm":["-cp","${classpath}"],"game":["--username","${auth_player_name}","--accessToken","${auth_access_token}"]}}`
	var v VersionJSON
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatal(err)
	}
	a, _ := OfflineAccount("Steve")
	a.AccessToken = "secret-token"
	cmd, err := BuildCommand(dirs, &v, LaunchOptions{Java: "java", GameDir: filepath.Join(dirs.Root, "game"), Account: a, MemoryMB: 2048, ExtraJVMArgs: []string{"-Dx=1"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"java", "-Xmx2048M", "-Dx=1", "-cp", dirs.VersionJar("1.21.1"), "net.minecraft.client.main.Main", "--username", "Steve", "--accessToken", "secret-token"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("got  %v\nwant %v", cmd.Args, want)
	}
	if s := DescribeCommand(cmd, "secret-token"); strings.Contains(s, "secret-token") {
		t.Fatal("token leaked in description")
	}
}
