package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type presetFile struct {
	Version string            `json:"version" yaml:"version"`
	Presets map[string]preset `json:"presets" yaml:"presets"`
}

type preset struct {
	ItunesXML             string `json:"itunes_xml" yaml:"itunes_xml"`
	ItunesRoot            string `json:"itunes_root" yaml:"itunes_root"`
	SubsonicRoot          string `json:"subsonic_root" yaml:"subsonic_root"`
	MusicRoot             string `json:"music_root" yaml:"music_root"`
	SubsonicURL           string `json:"subsonic_url" yaml:"subsonic_url"`
	SubsonicUser          string `json:"subsonic_user" yaml:"subsonic_user"`
	SubsonicPass          string `json:"subsonic_pass" yaml:"subsonic_pass"`
	SubsonicClient        string `json:"subsonic_client" yaml:"subsonic_client"`
	MatchMode             string `json:"match_mode" yaml:"match_mode"`
	RequireRealPath       *bool  `json:"require_real_path" yaml:"require_real_path"`
	VerifySrcFiles        *bool  `json:"verify_src_files" yaml:"verify_src_files"`
	DryRun                *bool  `json:"dry_run" yaml:"dry_run"`
	Debug                 *bool  `json:"debug" yaml:"debug"`
	RunDir                string `json:"run_dir" yaml:"run_dir"`
	ReportSyncPlan        string `json:"report_sync_plan" yaml:"report_sync_plan"`
	ReportSyncPlanTSV     string `json:"report_sync_plan_tsv" yaml:"report_sync_plan_tsv"`
	ReportReconcile       string `json:"report_reconcile" yaml:"report_reconcile"`
	NavidromeDump         string `json:"navidrome_dump" yaml:"navidrome_dump"`
	ReportLibrary         string `json:"report_library_stats" yaml:"report_library_stats"`
	ReportOutTSV          string `json:"out_tsv" yaml:"out_tsv"`
	ReportRemoteMatchJSON string `json:"report_remote_match_json" yaml:"report_remote_match_json"`
	ReportRemoteMatchTSV  string `json:"report_remote_match_tsv" yaml:"report_remote_match_tsv"`
	ReportRemoteActionTSV string `json:"report_remote_actionable_tsv" yaml:"report_remote_actionable_tsv"`
}

type resolvedPreset struct {
	PresetName            string            `json:"preset_name"`
	ItunesXML             string            `json:"itunes_xml"`
	ItunesRoot            string            `json:"itunes_root"`
	SubsonicRoot          string            `json:"subsonic_root"`
	MusicRoot             string            `json:"music_root"`
	SubsonicURL           string            `json:"subsonic_url"`
	SubsonicUser          string            `json:"subsonic_user"`
	SubsonicPass          string            `json:"subsonic_pass"`
	SubsonicClient        string            `json:"subsonic_client"`
	MatchMode             string            `json:"match_mode"`
	RequireRealPath       bool              `json:"require_real_path"`
	VerifySrcFiles        bool              `json:"verify_src_files"`
	DryRun                bool              `json:"dry_run"`
	Debug                 bool              `json:"debug"`
	RunDir                string            `json:"run_dir"`
	ReportSyncPlan        string            `json:"report_sync_plan"`
	ReportSyncPlanTSV     string            `json:"report_sync_plan_tsv"`
	ReportReconcile       string            `json:"report_reconcile"`
	NavidromeDump         string            `json:"navidrome_dump"`
	ReportLibrary         string            `json:"report_library_stats"`
	ReportOutTSV          string            `json:"out_tsv"`
	ReportRemoteMatchJSON string            `json:"report_remote_match_json"`
	ReportRemoteMatchTSV  string            `json:"report_remote_match_tsv"`
	ReportRemoteActionTSV string            `json:"report_remote_actionable_tsv"`
	Sources               map[string]string `json:"sources"`
}

func loadPresetFile(path string) (presetFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return presetFile{}, err
	}
	var cfg presetFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return presetFile{}, err
	}
	return cfg, nil
}

func resolvePreset(name string, cfg presetFile) (preset, error) {
	if cfg.Presets == nil {
		return preset{}, fmt.Errorf("no presets defined in config")
	}
	value, ok := cfg.Presets[name]
	if !ok {
		return preset{}, fmt.Errorf("preset %q not found", name)
	}
	return value, nil
}

func applyPreset(p preset, setFlags map[string]bool) {
	if !setFlags["itunes_xml"] && p.ItunesXML != "" {
		*itunesXml = p.ItunesXML
	}
	if !setFlags["itunes_root"] && p.ItunesRoot != "" {
		*itunesRoot = p.ItunesRoot
	}
	if !setFlags["subsonic_root"] && p.SubsonicRoot != "" {
		*subsonicRoot = p.SubsonicRoot
	}
	if !setFlags["music_root"] && p.MusicRoot != "" {
		*musicRoot = p.MusicRoot
	}
	if !setFlags["subsonic"] && p.SubsonicURL != "" {
		*subsonicUrl = p.SubsonicURL
	}
	if !setFlags["subsonic_client"] && p.SubsonicClient != "" {
		*subsonicClient = p.SubsonicClient
	}
	if !setFlags["match_mode"] && p.MatchMode != "" {
		*matchMode = p.MatchMode
	}
	if !setFlags["require_real_path"] && p.RequireRealPath != nil {
		*requireRealPath = *p.RequireRealPath
	}
	if !setFlags["verify_src_files"] && p.VerifySrcFiles != nil {
		*verifySrcFiles = *p.VerifySrcFiles
	}
	if !setFlags["dry_run"] && p.DryRun != nil {
		*dryRun = *p.DryRun
	}
	if !setFlags["debug"] && p.Debug != nil {
		*debugMode = *p.Debug
	}
	if !setFlags["run_dir"] && p.RunDir != "" {
		*runDir = p.RunDir
	}
	if !setFlags["report_sync_plan"] && p.ReportSyncPlan != "" {
		*reportSyncPlan = p.ReportSyncPlan
	}
	if !setFlags["report_sync_plan_tsv"] && p.ReportSyncPlanTSV != "" {
		*reportSyncPlanTSV = p.ReportSyncPlanTSV
	}
	if !setFlags["report_reconcile"] && p.ReportReconcile != "" {
		*reportReconcile = p.ReportReconcile
	}
	if !setFlags["navidrome_dump"] && p.NavidromeDump != "" {
		*dumpFile = p.NavidromeDump
	}
	if !setFlags["report_library_stats"] && p.ReportLibrary != "" {
		*reportLibrary = p.ReportLibrary
	}
	if !setFlags["out_tsv"] && p.ReportOutTSV != "" {
		*reportOutTSV = p.ReportOutTSV
	}
	if !setFlags["report_remote_match_json"] && p.ReportRemoteMatchJSON != "" {
		*reportRemoteMatchJSON = p.ReportRemoteMatchJSON
	}
	if !setFlags["report_remote_match_tsv"] && p.ReportRemoteMatchTSV != "" {
		*reportRemoteMatchTSV = p.ReportRemoteMatchTSV
	}
	if !setFlags["report_remote_actionable_tsv"] && p.ReportRemoteActionTSV != "" {
		*reportRemoteActionable = p.ReportRemoteActionTSV
	}
}

func buildResolvedPreset(name string, p preset, setFlags map[string]bool, cfg appConfig) resolvedPreset {
	requireRealPathValue := *requireRealPath
	if p.RequireRealPath != nil {
		requireRealPathValue = *p.RequireRealPath
	}
	dryRunValue := *dryRun
	if p.DryRun != nil {
		dryRunValue = *p.DryRun
	}
	debugValue := *debugMode
	if p.Debug != nil {
		debugValue = *p.Debug
	}
	resolvedSubsonicURL := *subsonicUrl
	if resolvedSubsonicURL == "" {
		resolvedSubsonicURL = cfg.SubsonicURL
	}
	resolvedSubsonicUser := firstNonEmpty(os.Getenv("SUBSONIC_USER"), p.SubsonicUser, cfg.SubsonicUser)
	resolvedSubsonicPass := firstNonEmpty(os.Getenv("SUBSONIC_PASS"), p.SubsonicPass, cfg.SubsonicPass)
	sources := presetSourceMap(p, setFlags, cfg)
	return resolvedPreset{
		PresetName:            name,
		ItunesXML:             *itunesXml,
		ItunesRoot:            *itunesRoot,
		SubsonicRoot:          *subsonicRoot,
		MusicRoot:             *musicRoot,
		SubsonicURL:           resolvedSubsonicURL,
		SubsonicUser:          resolvedSubsonicUser,
		SubsonicPass:          resolvedSubsonicPass,
		SubsonicClient:        *subsonicClient,
		MatchMode:             *matchMode,
		RequireRealPath:       requireRealPathValue,
		VerifySrcFiles:        *verifySrcFiles,
		DryRun:                dryRunValue,
		Debug:                 debugValue,
		RunDir:                *runDir,
		ReportSyncPlan:        *reportSyncPlan,
		ReportSyncPlanTSV:     *reportSyncPlanTSV,
		ReportReconcile:       *reportReconcile,
		NavidromeDump:         *dumpFile,
		ReportLibrary:         *reportLibrary,
		ReportOutTSV:          *reportOutTSV,
		ReportRemoteMatchJSON: *reportRemoteMatchJSON,
		ReportRemoteMatchTSV:  *reportRemoteMatchTSV,
		ReportRemoteActionTSV: *reportRemoteActionable,
		Sources:               sources,
	}
}

func presetSourceMap(p preset, setFlags map[string]bool, cfg appConfig) map[string]string {
	sources := make(map[string]string)
	sources["itunes_xml"] = sourceForString("itunes_xml", setFlags, p.ItunesXML, "")
	sources["itunes_root"] = sourceForString("itunes_root", setFlags, p.ItunesRoot, "")
	sources["subsonic_root"] = sourceForString("subsonic_root", setFlags, p.SubsonicRoot, "")
	sources["music_root"] = sourceForString("music_root", setFlags, p.MusicRoot, "")
	sources["subsonic_url"] = sourceForString("subsonic", setFlags, p.SubsonicURL, cfg.SubsonicURL)
	sources["subsonic_client"] = sourceForString("subsonic_client", setFlags, p.SubsonicClient, "")
	sources["match_mode"] = sourceForString("match_mode", setFlags, p.MatchMode, "")
	sources["require_real_path"] = sourceForBool("require_real_path", setFlags, p.RequireRealPath)
	sources["verify_src_files"] = sourceForBool("verify_src_files", setFlags, p.VerifySrcFiles)
	sources["dry_run"] = sourceForBool("dry_run", setFlags, p.DryRun)
	sources["debug"] = sourceForBool("debug", setFlags, p.Debug)
	sources["run_dir"] = sourceForString("run_dir", setFlags, p.RunDir, "")
	sources["report_sync_plan"] = sourceForString("report_sync_plan", setFlags, p.ReportSyncPlan, "")
	sources["report_sync_plan_tsv"] = sourceForString("report_sync_plan_tsv", setFlags, p.ReportSyncPlanTSV, "")
	sources["report_reconcile"] = sourceForString("report_reconcile", setFlags, p.ReportReconcile, "")
	sources["navidrome_dump"] = sourceForString("navidrome_dump", setFlags, p.NavidromeDump, "")
	sources["report_library_stats"] = sourceForString("report_library_stats", setFlags, p.ReportLibrary, "")
	sources["out_tsv"] = sourceForString("out_tsv", setFlags, p.ReportOutTSV, "")
	sources["report_remote_match_json"] = sourceForString("report_remote_match_json", setFlags, p.ReportRemoteMatchJSON, "")
	sources["report_remote_match_tsv"] = sourceForString("report_remote_match_tsv", setFlags, p.ReportRemoteMatchTSV, "")
	sources["report_remote_actionable_tsv"] = sourceForString("report_remote_actionable_tsv", setFlags, p.ReportRemoteActionTSV, "")
	sources["subsonic_user"] = sourceForSecret("SUBSONIC_USER", p.SubsonicUser, cfg.SubsonicUser)
	sources["subsonic_pass"] = sourceForSecret("SUBSONIC_PASS", p.SubsonicPass, cfg.SubsonicPass)
	return sources
}

func sourceForString(flagName string, setFlags map[string]bool, presetValue string, configValue string) string {
	if setFlags[flagName] {
		return "cli"
	}
	if presetValue != "" {
		return "preset"
	}
	if configValue != "" {
		return "config"
	}
	return "default"
}

func sourceForBool(flagName string, setFlags map[string]bool, presetValue *bool) string {
	if setFlags[flagName] {
		return "cli"
	}
	if presetValue != nil {
		return "preset"
	}
	return "default"
}

func sourceForSecret(envKey string, presetValue string, configValue string) string {
	if os.Getenv(envKey) != "" {
		return "env"
	}
	if presetValue != "" {
		return "preset"
	}
	if configValue != "" {
		return "config"
	}
	return "default"
}

func writeResolvedPreset(value resolvedPreset) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdoutWriter, string(payload))
	return nil
}

func checkPresetPlaceholders(p preset, setFlags map[string]bool, cfg appConfig) error {
	placeholders := make([]string, 0)
	check := func(label string, flagName string, value string) {
		if value == "" || setFlags[flagName] {
			return
		}
		if looksLikePlaceholder(value) {
			placeholders = append(placeholders, fmt.Sprintf("%s=%q", label, value))
		}
	}
	check("itunes_xml", "itunes_xml", p.ItunesXML)
	check("itunes_root", "itunes_root", p.ItunesRoot)
	check("subsonic_root", "subsonic_root", p.SubsonicRoot)
	check("music_root", "music_root", p.MusicRoot)
	check("navidrome_dump", "navidrome_dump", p.NavidromeDump)
	check("report_sync_plan", "report_sync_plan", p.ReportSyncPlan)
	check("report_sync_plan_tsv", "report_sync_plan_tsv", p.ReportSyncPlanTSV)
	check("report_reconcile", "report_reconcile", p.ReportReconcile)
	check("report_remote_match_json", "report_remote_match_json", p.ReportRemoteMatchJSON)
	check("report_remote_match_tsv", "report_remote_match_tsv", p.ReportRemoteMatchTSV)
	check("report_remote_actionable_tsv", "report_remote_actionable_tsv", p.ReportRemoteActionTSV)

	if !setFlags["subsonic"] {
		value := firstNonEmpty(p.SubsonicURL, cfg.SubsonicURL)
		if looksLikePlaceholder(value) {
			placeholders = append(placeholders, fmt.Sprintf("%s=%q", "subsonic_url", value))
		}
	}
	if len(placeholders) > 0 {
		return fmt.Errorf(strings.Join(placeholders, ", "))
	}
	return nil
}

func looksLikePlaceholder(value string) bool {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "/users/me/"),
		strings.Contains(lower, "navidrome.example.com"),
		strings.Contains(lower, "example.com"),
		strings.Contains(lower, "/path/to/"):
		return true
	default:
		return false
	}
}
