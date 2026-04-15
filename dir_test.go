package main

import (
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/glob"
)

// dirBin is the path to the compiled dir binary used for CLI-level tests.
var dirBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "dir-test-bin-*")
	if err != nil {
		panic("failed to create temp dir for binary: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	dirBin = filepath.Join(tmp, "dir")
	out, err := exec.Command("go", "build", "-o", dirBin, ".").CombinedOutput()
	if err != nil {
		panic("failed to build dir binary: " + string(out))
	}

	os.Exit(m.Run())
}

// runDir runs the compiled binary with the given args and returns combined output.
// It does not fail the test on non-zero exit — callers inspect output themselves.
func runDir(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(dirBin, args...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// assetsDir returns the absolute path to test_assets.
func assetsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("test_assets")
	if err != nil {
		t.Fatalf("could not resolve test_assets: %v", err)
	}
	return dir
}

// isLFSPointer reports whether path is a Git LFS pointer stub rather than the
// real file. LFS stubs start with the literal text "version https://git-lfs".
func isLFSPointer(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 24)
	n, _ := f.Read(buf)
	return strings.HasPrefix(string(buf[:n]), "version https://git-lfs")
}

// skipIfLFS calls t.Skip when any of the given paths is an LFS pointer.
func skipIfLFS(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if isLFSPointer(p) {
			t.Skipf("skipping: %s is a Git LFS pointer (run 'git lfs pull' to fetch real assets)", p)
		}
	}
}

func resetFileConditionGlobalsForTest() {
	listdirectories = true
	listfiles = true
	exclude_exts = nil
	exclude_dir_globs = nil
	listhidden = true
	mindate = time.Time{}
	maxdate = time.Time{}
	minmaxdatetype = "m"
	minsize = -1
	maxsize = math.MaxInt64
	only_executables = false
	listFoundText = false
	text_search_type = SEARCH_NONE
	text_regex = nil
	listInArchives = false
	pw7zip = ""
	pathIsArchive = false
	recurse_directories = false
	directory_header = true
	size_calculations = true
	use_colors = false
	use_enhanced_colors = false
	show_column_headers = false
	TotalFiles = 0
	TotalBytes = 0
	case_sensitive = false
	haveGlobber = false
	file_mask = ""
	matcher = nil
	skipArchiveEntryMask = false
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer failed: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout failed: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("closing pipe reader failed: %v", err)
	}
	return string(out)
}

// ── CLI: basic listing & recursion ───────────────────────────────────────────

func TestCLI_BasicListing(t *testing.T) {
	out := runDir(t, assetsDir(t))
	for _, want := range []string{"test.zip", "tgz_test.tgz", "Test Doc.docx"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q\n%s", want, out)
		}
	}
}

func TestCLI_RecursionShowsSubdir(t *testing.T) {
	out := runDir(t, "-r", assetsDir(t))
	if !strings.Contains(out, "recurse_only.txt") {
		t.Errorf("expected recursive listing to include recurse_only.txt\n%s", out)
	}
}

// ── CLI: -xd directory exclusion ─────────────────────────────────────────────

func TestCLI_ExcludeDirExact(t *testing.T) {
	out := runDir(t, "-r", "-xd=subdir", assetsDir(t))
	if strings.Contains(out, "recurse_only.txt") {
		t.Errorf("expected -xd=subdir to exclude subdir, but recurse_only.txt appeared\n%s", out)
	}
	if !strings.Contains(out, "test.zip") {
		t.Errorf("expected top-level files to still appear with -xd=subdir\n%s", out)
	}
}

func TestCLI_ExcludeDirGlobWildcard(t *testing.T) {
	// "sub*" should match "subdir" via glob and exclude it.
	out := runDir(t, "-r", "-xd=sub*", assetsDir(t))
	if strings.Contains(out, "recurse_only.txt") {
		t.Errorf("expected -xd=sub* to exclude subdir, but recurse_only.txt appeared\n%s", out)
	}
	if !strings.Contains(out, "test.zip") {
		t.Errorf("expected top-level files to still appear with -xd=sub*\n%s", out)
	}
}

func TestCLI_ExcludeDotFolders(t *testing.T) {
	// Build a temp tree: root/.hidden/secret.txt and root/visible/visible.txt
	root := t.TempDir()
	dotDir := filepath.Join(root, ".hidden")
	normalDir := filepath.Join(root, "visible")
	if err := os.MkdirAll(dotDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(normalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotDir, "secret.txt"), []byte("hidden"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(normalDir, "visible.txt"), []byte("visible"), 0644); err != nil {
		t.Fatal(err)
	}

	// exec.Command passes args directly — no shell expansion of ".*"
	out := runDir(t, "-r", "-xd=.*", root)
	if strings.Contains(out, "secret.txt") {
		t.Errorf("expected -xd=.* to exclude .hidden, but secret.txt appeared\n%s", out)
	}
	if !strings.Contains(out, "visible.txt") {
		t.Errorf("expected visible.txt to appear with -xd=.*\n%s", out)
	}
}

// ── CLI: text search in plain files ──────────────────────────────────────────
//
// Asset map (from expected_results.txt + direct inspection):
//   random_text.txt    — "Dirk Dastardly", "moustache"
//   Test Doc.docx      — "Jack Spratsworthy", "Wisconsin", "cheesemaker"
//   Test Doc2.docx     — "Trever Cook"  (note: Trever, not Trevor)
//   Test Doc2.pdf      — "Trever Cook"
//   Why Dir.pptx       — "Betelgeuse", "Constructor", "bulldozer"
//   expected_results.txt — describes all of the above; appears in most searches
//   subdir/recurse_only.txt — "RECURSE_SENTINEL_2026"

func TestCLI_TextSearch_CaseInsensitive_PlainText(t *testing.T) {
	// "dastardly" appears in random_text.txt (as "Dastardly") — case-insensitive hit.
	out := runDir(t, "-ti=dastardly", assetsDir(t))
	if !strings.Contains(out, "random_text.txt") {
		t.Errorf("expected random_text.txt in output\n%s", out)
	}
}

func TestCLI_TextSearch_CaseSensitive_Hit(t *testing.T) {
	// "Dastardly" (capital D) exists — exact-case hit.
	out := runDir(t, "-tc=Dastardly", assetsDir(t))
	if !strings.Contains(out, "random_text.txt") {
		t.Errorf("expected random_text.txt with -tc=Dastardly\n%s", out)
	}
}

func TestCLI_TextSearch_CaseSensitive_Miss(t *testing.T) {
	// "dastardly" (all lowercase) does not exist in any file — no matches.
	out := runDir(t, "-tc=dastardly", assetsDir(t))
	if !strings.Contains(out, "0 Files") {
		t.Errorf("expected 0 Files for case-sensitive miss\n%s", out)
	}
}

func TestCLI_TextSearch_Regex(t *testing.T) {
	// Regex [Dd]irk matches both capitalizations; only random_text.txt has it outside expected_results.
	out := runDir(t, "-tr=[Dd]irk", assetsDir(t))
	if !strings.Contains(out, "random_text.txt") {
		t.Errorf("expected random_text.txt with regex [Dd]irk\n%s", out)
	}
}

func TestCLI_TextSearch_Regex_NoMatch(t *testing.T) {
	// A regex that cannot match anything.
	out := runDir(t, "-tr=ZZZNONEXISTENTZZZZ", assetsDir(t))
	if !strings.Contains(out, "0 Files") {
		t.Errorf("expected 0 Files for non-matching regex\n%s", out)
	}
}

func TestCLI_TextSearch_Docx(t *testing.T) {
	// "spratsworthy" is unique to Test Doc.docx.
	out := runDir(t, "-ti=spratsworthy", assetsDir(t))
	if !strings.Contains(out, "Test Doc.docx") {
		t.Errorf("expected Test Doc.docx in output\n%s", out)
	}
}

func TestCLI_TextSearch_Docx_CaseSensitive(t *testing.T) {
	// "Spratsworthy" — exact-case hit in the docx.
	out := runDir(t, "-tc=Spratsworthy", assetsDir(t))
	if !strings.Contains(out, "Test Doc.docx") {
		t.Errorf("expected Test Doc.docx with -tc=Spratsworthy\n%s", out)
	}
}

func TestCLI_TextSearch_Pdf(t *testing.T) {
	// "trever" is the spelling in Test Doc2.docx and Test Doc2.pdf.
	out := runDir(t, "-ti=trever", assetsDir(t))
	if !strings.Contains(out, "Test Doc2.docx") {
		t.Errorf("expected Test Doc2.docx\n%s", out)
	}
	if !strings.Contains(out, "Test Doc2.pdf") {
		t.Errorf("expected Test Doc2.pdf\n%s", out)
	}
}

func TestCLI_TextSearch_Pptx(t *testing.T) {
	// "bulldozer" is unique to Why Dir.pptx (+ expected_results.txt).
	out := runDir(t, "-ti=bulldozer", assetsDir(t))
	if !strings.Contains(out, "Why Dir.pptx") {
		t.Errorf("expected Why Dir.pptx in output\n%s", out)
	}
}

func TestCLI_TextSearch_Pptx_Regex(t *testing.T) {
	// Anchored regex matching either "bulldozer" or "Bulldozer".
	out := runDir(t, "-tr=[Bb]ulldozer", assetsDir(t))
	if !strings.Contains(out, "Why Dir.pptx") {
		t.Errorf("expected Why Dir.pptx with regex [Bb]ulldozer\n%s", out)
	}
}

// ── CLI: text search with recursion ──────────────────────────────────────────

func TestCLI_TextSearch_Recursive_Subdir(t *testing.T) {
	// RECURSE_SENTINEL_2026 exists only in subdir/recurse_only.txt.
	out := runDir(t, "-r", "-ti=RECURSE_SENTINEL_2026", assetsDir(t))
	if !strings.Contains(out, "recurse_only.txt") {
		t.Errorf("expected recurse_only.txt in recursive search\n%s", out)
	}
	// Top-level directory should show 0 matches — sentinel is only in the subdir.
	if strings.Contains(out, "test.zip") || strings.Contains(out, "random_text.txt") {
		t.Errorf("unexpected top-level file appeared for sentinel search\n%s", out)
	}
}

func TestCLI_TextSearch_RecursiveExcludeSubdir(t *testing.T) {
	// With -xd=subdir, the sentinel should not be found even with -r.
	out := runDir(t, "-r", "-xd=subdir", "-ti=RECURSE_SENTINEL_2026", assetsDir(t))
	if strings.Contains(out, "recurse_only.txt") {
		t.Errorf("expected recurse_only.txt to be excluded by -xd=subdir\n%s", out)
	}
}

// ── CLI: text search inside archives (-z) ────────────────────────────────────

func TestCLI_TextSearch_ZipArchive_Docx(t *testing.T) {
	skipIfLFS(t, "test_assets/test.zip")
	// "spratsworthy" is in Test File for Dir.docx inside test.zip.
	out := runDir(t, "-z", "-ti=spratsworthy", assetsDir(t))
	if !strings.Contains(out, "Test File for Dir.docx") {
		t.Errorf("expected Test File for Dir.docx from test.zip\n%s", out)
	}
}

func TestCLI_TextSearch_TgzArchive_PlainText(t *testing.T) {
	skipIfLFS(t, "test_assets/tgz_test.tgz")
	// random_text.txt (containing "dastardly") is stored inside tgz_test.tgz.
	out := runDir(t, "-z", "-ti=dastardly", assetsDir(t))
	if !strings.Contains(out, "tgz_test.tgz") {
		t.Errorf("expected tgz_test.tgz section in output\n%s", out)
	}
	// The match inside the archive should be random_text.txt.
	if !strings.Contains(out, "random_text.txt") {
		t.Errorf("expected random_text.txt match inside tgz\n%s", out)
	}
}

func TestCLI_TextSearch_TgzArchive_Docx(t *testing.T) {
	skipIfLFS(t, "test_assets/tgz_test.tgz")
	// Test File for Dir2.docx (Trever Cook) is inside tgz_test.tgz.
	out := runDir(t, "-z", "-ti=trever", assetsDir(t))
	if !strings.Contains(out, "Test File for Dir2.docx") {
		t.Errorf("expected Test File for Dir2.docx from tgz\n%s", out)
	}
}

func TestCLI_TextSearch_7zArchive_Pptx(t *testing.T) {
	skipIfLFS(t, "test_assets/sz_test.7z")
	// Why Dir.pptx (containing "Betelgeuse") is inside sz_test.7z.
	out := runDir(t, "-z", "-ti=betelgeuse", assetsDir(t))
	if !strings.Contains(out, "sz_test.7z") {
		t.Errorf("expected sz_test.7z section in output\n%s", out)
	}
	if !strings.Contains(out, "Why Dir.pptx") {
		t.Errorf("expected Why Dir.pptx match inside sz_test.7z\n%s", out)
	}
}

func TestCLI_TextSearch_7zArchive_PlainText(t *testing.T) {
	skipIfLFS(t, "test_assets/sz_test.7z")
	// random_text.txt is stored in sz_test.7z; "moustache" is unique to that file.
	out := runDir(t, "-z", "-ti=moustache", assetsDir(t))
	if !strings.Contains(out, "sz_test.7z") {
		t.Errorf("expected sz_test.7z section in output\n%s", out)
	}
}

func TestCLI_TextSearch_Encrypted7z(t *testing.T) {
	skipIfLFS(t, "test_assets/encrypted.7z")
	// Both EncDoc.docx and EncPDF.pdf contain "encrypted"; password required.
	out := runDir(t, "-z", "-zpw=password", "-ti=encrypted", assetsDir(t))
	if !strings.Contains(out, "EncDoc.docx") {
		t.Errorf("expected EncDoc.docx in output\n%s", out)
	}
	if !strings.Contains(out, "EncPDF.pdf") {
		t.Errorf("expected EncPDF.pdf in output\n%s", out)
	}
}

func TestCLI_TextSearch_Encrypted7z_Regex(t *testing.T) {
	skipIfLFS(t, "test_assets/encrypted.7z")
	// Same as above but using regex mode.
	out := runDir(t, "-z", "-zpw=password", "-tr=(?i)encrypted", assetsDir(t))
	if !strings.Contains(out, "EncDoc.docx") {
		t.Errorf("expected EncDoc.docx in output with regex\n%s", out)
	}
}

func TestCLI_TextSearch_RecursiveAndArchives(t *testing.T) {
	skipIfLFS(t, "test_assets/sz_test.7z", "test_assets/tgz_test.tgz")
	// -r -z together: "dastardly" appears in the flat random_text.txt, inside
	// tgz_test.tgz, and inside sz_test.7z.
	out := runDir(t, "-r", "-z", "-ti=dastardly", assetsDir(t))
	if !strings.Contains(out, "random_text.txt") {
		t.Errorf("expected flat random_text.txt\n%s", out)
	}
	if !strings.Contains(out, "tgz_test.tgz") {
		t.Errorf("expected tgz section\n%s", out)
	}
	if !strings.Contains(out, "sz_test.7z") {
		t.Errorf("expected sz_test.7z section\n%s", out)
	}
}

// ── Unit tests ────────────────────────────────────────────────────────────────

func TestFileMeetsConditions_AppliesMaskToArchiveEntriesByDefault(t *testing.T) {
	resetFileConditionGlobalsForTest()
	haveGlobber = true
	matcher = glob.MustCompile("ENCRYPTED.7Z")

	target := fileitem{Name: "EncDoc.docx", InArchive: true, Size: 100}
	ok, _ := fileMeetsConditions(target, false)
	if ok {
		t.Fatalf("expected archive entry to be filtered by outer mask when skipArchiveEntryMask is false")
	}
}

func TestFileMeetsConditions_SkipsMaskForArchiveEntriesWhenEnabled(t *testing.T) {
	resetFileConditionGlobalsForTest()
	haveGlobber = true
	matcher = glob.MustCompile("ENCRYPTED.7Z")
	skipArchiveEntryMask = true

	target := fileitem{Name: "EncDoc.docx", InArchive: true, Size: 100}
	ok, _ := fileMeetsConditions(target, false)
	if !ok {
		t.Fatalf("expected archive entry to pass when skipArchiveEntryMask is true")
	}
}

func TestFileMeetsConditions_ArchiveContainerBypassesTextScanWhenNameMasked(t *testing.T) {
	resetFileConditionGlobalsForTest()
	haveGlobber = true
	matcher = glob.MustCompile("ENCRYPTED.7Z")
	listInArchives = true
	text_search_type = SEARCH_NOCASE
	text_regex = regexp.MustCompile("(?i)enc")

	target := fileitem{Name: "encrypted.7z", Size: 100}
	ok, _ := fileMeetsConditions(target, false)
	if !ok {
		t.Fatalf("expected targeted archive container to pass and be listed")
	}
}

func TestListDirectory_TargetedArchiveRecursesAndListsRootAndEntries(t *testing.T) {
	skipIfLFS(t, "test_assets/encrypted.7z")
	resetFileConditionGlobalsForTest()
	listInArchives = true
	recurse_directories = true
	text_search_type = SEARCH_NOCASE
	text_regex = regexp.MustCompile("(?i)enc")
	pw7zip = "password"
	haveGlobber = true
	matcher = glob.MustCompile("ENCRYPTED.7Z")

	assets, err := filepath.Abs("test_assets")
	if err != nil {
		t.Fatalf("could not resolve test_assets path: %v", err)
	}

	out := captureStdout(t, func() {
		if err := list_directory(assets, false, false); err != nil {
			t.Fatalf("list_directory failed: %v", err)
		}
	})

	archivePath := filepath.Join(assets, "encrypted.7z")
	for _, want := range []string{
		"Directory of " + assets,
		"encrypted.7z",
		"Directory of " + archivePath,
		"EncDoc.docx",
		"EncPDF.pdf",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func normalizeListingSetForCompare(ls ListingSet) ListingSet {
	sort.Slice(ls.MatchedFiles, func(i, j int) bool {
		if ls.MatchedFiles[i].Path != ls.MatchedFiles[j].Path {
			return ls.MatchedFiles[i].Path < ls.MatchedFiles[j].Path
		}
		if ls.MatchedFiles[i].Name != ls.MatchedFiles[j].Name {
			return ls.MatchedFiles[i].Name < ls.MatchedFiles[j].Name
		}
		return ls.MatchedFiles[i].Size < ls.MatchedFiles[j].Size
	})
	sort.Strings(ls.Subdirs)
	sort.Strings(ls.Archives)
	return ls
}

func TestLinearFilesIn7ZArchive_MatchesFilesIn7ZArchive(t *testing.T) {
	cases := []struct {
		name           string
		archive        string
		password       string
		textSearchType searchtype
		textRegex      *regexp.Regexp
		listAllMatches bool
	}{
		{
			name:           "no text search",
			archive:        "test_assets/sz_test.7z",
			textSearchType: SEARCH_NONE,
		},
		{
			name:           "text search with all matches",
			archive:        "test_assets/sz_test.7z",
			textSearchType: SEARCH_NOCASE,
			textRegex:      regexp.MustCompile("(?i)test"),
			listAllMatches: true,
		},
		{
			name:           "encrypted archive text search",
			archive:        "test_assets/encrypted.7z",
			password:       "password",
			textSearchType: SEARCH_NOCASE,
			textRegex:      regexp.MustCompile("(?i)enc"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			skipIfLFS(t, tc.archive)
			resetFileConditionGlobalsForTest()
			text_search_type = tc.textSearchType
			text_regex = tc.textRegex
			listFoundText = tc.listAllMatches
			pw7zip = tc.password

			archivePath, err := filepath.Abs(tc.archive)
			if err != nil {
				t.Fatalf("could not resolve archive path: %v", err)
			}

			gotLinear, err := linearFilesIn7ZArchive(archivePath)
			if err != nil {
				t.Fatalf("linearFilesIn7ZArchive failed: %v", err)
			}
			gotLegacy, err := filesIn7ZArchive(archivePath)
			if err != nil {
				t.Fatalf("filesIn7ZArchive failed: %v", err)
			}

			gotLinear = normalizeListingSetForCompare(gotLinear)
			gotLegacy = normalizeListingSetForCompare(gotLegacy)

			if !reflect.DeepEqual(gotLinear, gotLegacy) {
				t.Fatalf("listing mismatch\nlinear=%#v\nlegacy=%#v", gotLinear, gotLegacy)
			}
		})
	}
}

func TestSanitizeTempPattern_RemovesPathSeparators(t *testing.T) {
	got := sanitizeTempPattern("ALL KENT CHEN/20240125-1216 - .pdf")
	if strings.Contains(got, "/") || strings.Contains(got, "\\") {
		t.Fatalf("expected no path separators in temp pattern, got %q", got)
	}
}
