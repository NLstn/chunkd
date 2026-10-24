package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This test uses an actual JVM with a locally compiled fixture JAR. It does not
// download Minecraft, run its code or accept its EULA.
func TestActualJavaJarLifecycle(t *testing.T) {
	java, javaErr := exec.LookPath("java")
	javac, javacErr := exec.LookPath("javac")
	jar, jarErr := exec.LookPath("jar")
	if javaErr != nil || javacErr != nil || jarErr != nil {
		if os.Getenv("CHUNKD_REQUIRE_JAVA_TEST") == "1" {
			t.Fatal("JDK required for integration test")
		}
		t.Skip("JDK unavailable; CI requires the actual JVM test")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "FixtureServer.java")
	code := `import java.io.*;
public class FixtureServer {
  public static void main(String[] args) throws Exception {
    System.err.println("fixture JVM stderr");
    System.out.println("Done (0.01s)! For help, type help");
    BufferedReader input = new BufferedReader(new InputStreamReader(System.in));
    String line;
    while ((line = input.readLine()) != null) if (line.equals("stop")) return;
  }
}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []*exec.Cmd{exec.Command(javac, source), exec.Command(jar, "--create", "--file", filepath.Join(dir, "server.jar"), "--main-class", "FixtureServer", "-C", dir, "FixtureServer.class")} {
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("JDK fixture: %v %s", err, out)
		}
	}
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	events := make(chan Event, 8)
	if err := s.Start(99, Launch{Java: java, Directory: dir, MemoryMB: 512}, func(e Event) { events <- e }); err != nil {
		t.Fatal(err)
	}
	waitState(t, events, "Running")
	if err := s.Stop(context.Background(), 99); err != nil {
		t.Fatal(err)
	}
	waitState(t, events, "Stopped")
}
