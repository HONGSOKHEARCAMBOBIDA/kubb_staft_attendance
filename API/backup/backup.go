package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Filename  string    `json:"filename"`
	FilePath  string    `json:"file_path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func Run() (*Result, error) {
	savePath := "./backups"
	if err := os.MkdirAll(savePath, 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("backup_%s_%s.sql", os.Getenv("DB_NAME"), timestamp)
	filePath := filepath.Join(savePath, filename)

	// FIX: mysqldump -> pg_dump, with argument conventions changed to match:
	//   -u user           -> -U user   (capital U; -u doesn't exist in pg_dump)
	//   -p<password>       -> removed. In pg_dump, -p means PORT, not password —
	//                         reusing that flag here would make it try to parse
	//                         your password as a port number. Postgres tools take
	//                         the password via the PGPASSWORD env var instead
	//                         (set below on cmd.Env), never as a CLI argument.
	//   --routines/--triggers -> removed. These are MySQL-only flags that control
	//                         whether stored routines/triggers are dumped.
	//                         pg_dump includes functions and triggers by default,
	//                         so there's no equivalent flag needed.
	//
	// FIX 2: --single-transaction removed. The pg_dump binary actually being
	// resolved on PATH in this process's environment is rejecting it with
	// "illegal option -- single-transaction" — a getopt error style that
	// means this particular pg_dump build isn't parsing GNU-style long
	// options at all. That almost always means the process's PATH (service/
	// container/cron context) resolves to a different pg_dump than your
	// interactive shell does. See the LookPath diagnostic logged below —
	// once you confirm which binary is actually running, either point PATH
	// at the real PostgreSQL client tools (matching what worked manually)
	// or, if you must keep this older/limited binary, this flag has no safe
	// single-dash equivalent and has to stay omitted.
	pgDumpPath, lookErr := exec.LookPath("pg_dump")
	if lookErr != nil {
		return nil, fmt.Errorf("pg_dump not found on PATH: %w", lookErr)
	}
	fmt.Printf("using pg_dump binary: %s\n", pgDumpPath) // TODO: swap for your logger

	cmd := exec.Command(
		pgDumpPath,
		"-U", os.Getenv("DB_USER"),
		"-h", os.Getenv("DB_HOST"),
		"-p", os.Getenv("DB_PORT"),
		"-d", os.Getenv("DB_NAME"),
	)

	// FIX: password passed through the environment, the standard way
	// Postgres client tools expect it, rather than as a command-line flag
	// (which would also leak it in `ps` output on the host).
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", os.Getenv("DB_PASSWORD")))

	outFile, err := os.Create(filePath)
	// creates a file at the path specified by filePath
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile

	// pg_dump writes warnings/errors to stderr; capture them so a failure
	// actually tells you why instead of just "exit status 1".
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("pg_dump failed: %w: %s", err, stderr.String())
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat backup file: %w", err)
	}
	return &Result{
		Filename:  filename,
		FilePath:  filePath,
		SizeBytes: info.Size(),
		CreatedAt: time.Now(),
	}, nil
}

func DeleteOldBackups(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	entries, err := os.ReadDir("./backups")
	if err != nil {
		return err
	}
	for _, e := range entries {
		info, _ := e.Info()
		if !e.IsDir() && info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join("./backups", e.Name()))
		}
	}
	return nil
}
