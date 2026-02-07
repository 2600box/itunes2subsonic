package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type presetFile struct {
	Version string            `json:"version" yaml:"version"`
	Presets map[string]preset `json:"presets" yaml:"presets"`
}

type preset struct {
	ItunesXML         string `json:"itunes_xml" yaml:"itunes_xml"`
	ItunesRoot        string `json:"itunes_root" yaml:"itunes_root"`
	SubsonicRoot      string `json:"subsonic_root" yaml:"subsonic_root"`
	MusicRoot         string `json:"music_root" yaml:"music_root"`
	SubsonicURL       string `json:"subsonic_url" yaml:"subsonic_url"`
	SubsonicUser      string `json:"subsonic_user" yaml:"subsonic_user"`
	SubsonicPass      string `json:"subsonic_pass" yaml:"subsonic_pass"`
	SubsonicClient    string `json:"subsonic_client" yaml:"subsonic_client"`
	MatchMode         string `json:"match_mode" yaml:"match_mode"`
	RequireRealPath   *bool  `json:"require_real_path" yaml:"require_real_path"`
	ReportSyncPlan    string `json:"report_sync_plan" yaml:"report_sync_plan"`
	ReportSyncPlanTSV string `json:"report_sync_plan_tsv" yaml:"report_sync_plan_tsv"`
	ReportReconcile   string `json:"report_reconcile" yaml:"report_reconcile"`
	NavidromeDump     string `json:"navidrome_dump" yaml:"navidrome_dump"`
	ReportLibrary     string `json:"report_library_stats" yaml:"report_library_stats"`
	ReportOutTSV      string `json:"out_tsv" yaml:"out_tsv"`
}

type resolvedPreset struct {
	PresetName        string `json:"preset_name"`
	ItunesXML         string `json:"itunes_xml"`
	ItunesRoot        string `json:"itunes_root"`
	SubsonicRoot      string `json:"subsonic_root"`
	MusicRoot         string `json:"music_root"`
	SubsonicURL       string `json:"subsonic_url"`
	SubsonicUser      string `json:"subsonic_user"`
	SubsonicPass      string `json:"subsonic_pass"`
	SubsonicClient    string `json:"subsonic_client"`
	MatchMode         string `json:"match_mode"`
	RequireRealPath   bool   `json:"require_real_path"`
	ReportSyncPlan    string `json:"report_sync_plan"`
	ReportSyncPlanTSV string `json:"report_sync_plan_tsv"`
	ReportReconcile   string `json:"report_reconcile"`
	NavidromeDump     string `json:"navidrome_dump"`
	ReportLibrary     string `json:"report_library_stats"`
	ReportOutTSV      string `json:"out_tsv"`
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
}

func buildResolvedPreset(name string, p preset) resolvedPreset {
	requireRealPathValue := *requireRealPath
	if p.RequireRealPath != nil {
		requireRealPathValue = *p.RequireRealPath
	}
	return resolvedPreset{
		PresetName:        name,
		ItunesXML:         *itunesXml,
		ItunesRoot:        *itunesRoot,
		SubsonicRoot:      *subsonicRoot,
		MusicRoot:         *musicRoot,
		SubsonicURL:       *subsonicUrl,
		SubsonicUser:      p.SubsonicUser,
		SubsonicPass:      p.SubsonicPass,
		SubsonicClient:    *subsonicClient,
		MatchMode:         *matchMode,
		RequireRealPath:   requireRealPathValue,
		ReportSyncPlan:    *reportSyncPlan,
		ReportSyncPlanTSV: *reportSyncPlanTSV,
		ReportReconcile:   *reportReconcile,
		NavidromeDump:     *dumpFile,
		ReportLibrary:     *reportLibrary,
		ReportOutTSV:      *reportOutTSV,
	}
}

func writeResolvedPreset(value resolvedPreset) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdoutWriter, string(payload))
	return nil
}
