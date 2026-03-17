package ai

import (
	"strings"
	"testing"
)

func TestFilterDiffByFiles(t *testing.T) {
	diff := `diff --git a/auth/jwt.go b/auth/jwt.go
index abc..def 100644
--- a/auth/jwt.go
+++ b/auth/jwt.go
@@ -10,6 +10,7 @@
 func Validate() {
+	// added line
 }
diff --git a/handlers/user.go b/handlers/user.go
index 111..222 100644
--- a/handlers/user.go
+++ b/handlers/user.go
@@ -1,3 +1,4 @@
+// new handler
 package handlers
diff --git a/README.md b/README.md
index 333..444 100644
--- a/README.md
+++ b/README.md
@@ -1,2 +1,3 @@
+# heading
`

	result := FilterDiffByFiles(diff, []string{"auth/jwt.go", "README.md"})

	if !strings.Contains(result, "auth/jwt.go") {
		t.Error("expected auth/jwt.go in filtered diff")
	}
	if !strings.Contains(result, "README.md") {
		t.Error("expected README.md in filtered diff")
	}
	if strings.Contains(result, "handlers/user.go") {
		t.Error("expected handlers/user.go to be excluded")
	}
}

func TestFilterDiffByFiles_Empty(t *testing.T) {
	result := FilterDiffByFiles("", []string{"foo.go"})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestFilterDiffByFiles_NoMatch(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\nindex 1..2 100644\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+x\n"
	result := FilterDiffByFiles(diff, []string{"bar.go"})
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}
