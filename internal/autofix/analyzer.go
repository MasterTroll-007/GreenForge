package autofix

import (
	"regexp"
	"strings"

	"github.com/greencode/greenforge/internal/cicd"
)

// FailureAnalysis contains the analyzed root cause and suggested fix.
type FailureAnalysis struct {
	Category    string   `json:"category"`    // test_failure, compile_error, dependency, config, unknown
	RootCause   string   `json:"root_cause"`
	AffectedFiles []string `json:"affected_files"`
	Suggestion  string   `json:"suggestion"`
	Confidence  float64  `json:"confidence"` // 0.0-1.0
	CanAutoFix  bool     `json:"can_auto_fix"`
}

// AnalyzeFailure examines pipeline error logs and determines the failure type.
func AnalyzeFailure(p cicd.Pipeline) *FailureAnalysis {
	log := p.ErrorLog
	if log == "" {
		return &FailureAnalysis{
			Category:   "unknown",
			RootCause:  "No error log available",
			Confidence: 0.1,
			CanAutoFix: false,
		}
	}

	// Try each analyzer in order of specificity
	analyzers := []func(string) *FailureAnalysis{
		analyzeTestFailure,
		analyzeCompileError,
		analyzeDependencyError,
		analyzeConfigError,
		analyzeOutOfMemory,
		analyzeTimeout,
	}

	for _, analyze := range analyzers {
		if result := analyze(log); result != nil {
			return result
		}
	}

	return &FailureAnalysis{
		Category:   "unknown",
		RootCause:  truncate(log, 200),
		Confidence: 0.1,
		CanAutoFix: false,
	}
}

// --- Test failure patterns ---

var testFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(\w+(?:\.\w+)*Test)\.(\w+)\s.*(?:FAILED|FAILURE)`),
	regexp.MustCompile(`(?m)Tests run:.*Failures: (\d+)`),
	regexp.MustCompile(`(?m)(\w+).*(?:NullPointerException|NPE).*at (\S+)\((\S+):(\d+)\)`),
	regexp.MustCompile(`(?m)(?:AssertionError|AssertionFailedError|ComparisonFailure).*expected.*but was`),
	regexp.MustCompile(`(?m)java\.lang\.(\w+Exception).*at (\S+)`),
}

func analyzeTestFailure(log string) *FailureAnalysis {
	// Check for JUnit/TestNG patterns
	if m := testFailurePatterns[0].FindStringSubmatch(log); m != nil {
		return &FailureAnalysis{
			Category:   "test_failure",
			RootCause:  "Test failure: " + m[1] + "." + m[2],
			Suggestion: "Fix the failing test or the code it tests",
			Confidence: 0.8,
			CanAutoFix: true,
		}
	}

	// NPE in test
	if m := testFailurePatterns[2].FindStringSubmatch(log); m != nil {
		files := []string{}
		if len(m) > 3 {
			files = append(files, m[3])
		}
		return &FailureAnalysis{
			Category:      "test_failure",
			RootCause:     "NullPointerException at " + m[2],
			AffectedFiles: files,
			Suggestion:    "Add null check or fix null value source",
			Confidence:    0.7,
			CanAutoFix:    true,
		}
	}

	// General test failure count
	if m := testFailurePatterns[1].FindStringSubmatch(log); m != nil && m[1] != "0" {
		return &FailureAnalysis{
			Category:   "test_failure",
			RootCause:  m[1] + " test(s) failed",
			Suggestion: "Review and fix failing tests",
			Confidence: 0.6,
			CanAutoFix: true,
		}
	}

	// Assertion errors
	if testFailurePatterns[3].MatchString(log) {
		return &FailureAnalysis{
			Category:   "test_failure",
			RootCause:  "Assertion failure - expected value mismatch",
			Suggestion: "Update expected value or fix the logic",
			Confidence: 0.6,
			CanAutoFix: true,
		}
	}

	return nil
}

// --- Compile error patterns ---

var compilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(\S+\.(?:java|kt)):(\d+):.*error: (.*)`),
	regexp.MustCompile(`(?m)Compilation failed.*see the compiler error output`),
	regexp.MustCompile(`(?m)COMPILATION ERROR`),
	regexp.MustCompile(`(?m)cannot find symbol.*class (\w+)`),
	regexp.MustCompile(`(?m)Unresolved reference: (\w+)`),
}

func analyzeCompileError(log string) *FailureAnalysis {
	if m := compilePatterns[0].FindStringSubmatch(log); m != nil {
		return &FailureAnalysis{
			Category:      "compile_error",
			RootCause:     "Compilation error in " + m[1] + ":" + m[2] + " - " + m[3],
			AffectedFiles: []string{m[1]},
			Suggestion:    "Fix the compilation error",
			Confidence:    0.9,
			CanAutoFix:    false, // compile errors usually need human review
		}
	}

	if compilePatterns[1].MatchString(log) || compilePatterns[2].MatchString(log) {
		return &FailureAnalysis{
			Category:   "compile_error",
			RootCause:  "Compilation failed",
			Suggestion: "Review compiler errors and fix source code",
			Confidence: 0.8,
			CanAutoFix: false,
		}
	}

	if m := compilePatterns[3].FindStringSubmatch(log); m != nil {
		return &FailureAnalysis{
			Category:   "compile_error",
			RootCause:  "Missing class: " + m[1],
			Suggestion: "Add missing import or dependency",
			Confidence: 0.7,
			CanAutoFix: false,
		}
	}

	return nil
}

// --- Dependency errors ---

var dependencyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)Could not resolve.*(\S+:\S+:\S+)`),
	regexp.MustCompile(`(?m)Could not find artifact (\S+)`),
	regexp.MustCompile(`(?m)Dependency resolution failed`),
	regexp.MustCompile(`(?m)Could not download \S+`),
}

func analyzeDependencyError(log string) *FailureAnalysis {
	if m := dependencyPatterns[0].FindStringSubmatch(log); m != nil {
		return &FailureAnalysis{
			Category:   "dependency",
			RootCause:  "Cannot resolve dependency: " + m[1],
			Suggestion: "Check dependency version exists in configured repositories",
			Confidence: 0.8,
			CanAutoFix: false,
		}
	}

	if m := dependencyPatterns[1].FindStringSubmatch(log); m != nil {
		return &FailureAnalysis{
			Category:   "dependency",
			RootCause:  "Missing artifact: " + m[1],
			Suggestion: "Verify artifact coordinates and repository configuration",
			Confidence: 0.8,
			CanAutoFix: false,
		}
	}

	for _, pat := range dependencyPatterns[2:] {
		if pat.MatchString(log) {
			return &FailureAnalysis{
				Category:   "dependency",
				RootCause:  "Dependency resolution problem",
				Suggestion: "Check build configuration and repository access",
				Confidence: 0.6,
				CanAutoFix: false,
			}
		}
	}

	return nil
}

// --- Config errors ---

func analyzeConfigError(log string) *FailureAnalysis {
	configPatterns := []struct {
		pattern *regexp.Regexp
		msg     string
	}{
		{regexp.MustCompile(`(?m)ApplicationContextException.*Failed to start`), "Spring context failed to start"},
		{regexp.MustCompile(`(?m)BeanCreationException.*(\w+)`), "Spring bean creation failed"},
		{regexp.MustCompile(`(?m)Cannot determine embedded database.*url`), "Database URL not configured"},
		{regexp.MustCompile(`(?m)port.*already in use|Address already in use`), "Port conflict"},
	}

	for _, cp := range configPatterns {
		if cp.pattern.MatchString(log) {
			return &FailureAnalysis{
				Category:   "config",
				RootCause:  cp.msg,
				Suggestion: "Check application configuration and environment",
				Confidence: 0.7,
				CanAutoFix: false,
			}
		}
	}

	return nil
}

// --- Resource errors ---

func analyzeOutOfMemory(log string) *FailureAnalysis {
	if strings.Contains(log, "OutOfMemoryError") || strings.Contains(log, "GC overhead limit") {
		return &FailureAnalysis{
			Category:   "resource",
			RootCause:  "Out of memory (OOM)",
			Suggestion: "Increase JVM heap (-Xmx) or optimize memory usage",
			Confidence: 0.9,
			CanAutoFix: false,
		}
	}
	return nil
}

func analyzeTimeout(log string) *FailureAnalysis {
	if strings.Contains(log, "timed out") || strings.Contains(log, "timeout") || strings.Contains(log, "exceeded the time limit") {
		return &FailureAnalysis{
			Category:   "timeout",
			RootCause:  "Build/test timed out",
			Suggestion: "Increase timeout or optimize slow tests",
			Confidence: 0.7,
			CanAutoFix: false,
		}
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
