// SPDX-FileCopyrightText: 2026 Micheal Choudhary <mc@miche.al>
// SPDX-License-Identifier: MIT

package sbom

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/michealch/apt2distroless/internal/manifest"
)

// cdxDoc mirrors the CycloneDX 1.5 BOM JSON structure we need.
type cdxDoc struct {
	BOMFormat    string         `json:"bomFormat"`
	SpecVersion  string         `json:"specVersion"`
	SerialNumber string         `json:"serialNumber"`
	Version      int            `json:"version"`
	Metadata     cdxMetadata    `json:"metadata"`
	Components   []cdxComponent `json:"components"`
	Dependencies []cdxDep       `json:"dependencies"`
}

type cdxMetadata struct {
	Timestamp string    `json:"timestamp"`
	Tools     []cdxTool `json:"tools"`
}

type cdxTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type cdxComponent struct {
	BOMRef     string   `json:"bom-ref"`
	Type       string   `json:"type"`
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Supplier   *cdxOrg  `json:"supplier,omitempty"`
	PackageURL string   `json:"purl"`
	Licenses   []cdxLic `json:"licenses,omitempty"`
}

type cdxOrg struct {
	Name string `json:"name"`
}

type cdxLic struct {
	License cdxLicID `json:"license"`
}

type cdxLicID struct {
	ID string `json:"id,omitempty"`
}

type cdxDep struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

// WriteCycloneDX emits a CycloneDX 1.5 JSON BOM for the run result.
func WriteCycloneDX(path string, r *manifest.RunResult, d Distro, epoch int64) error {
	timestamp := time.Unix(epoch, 0).UTC().Format(time.RFC3339)

	bomRef := func(name string) string {
		return "pkg:" + strings.ReplaceAll(name, ":", "-")
	}

	comps := make([]cdxComponent, len(r.Packages))
	for i, p := range r.Packages {
		c := cdxComponent{
			BOMRef:     bomRef(p.Name),
			Type:       "library",
			Name:       p.Name,
			Version:    p.Version,
			PackageURL: PURL(&p, d),
		}
		if p.Maintainer != "" {
			c.Supplier = &cdxOrg{Name: p.Maintainer}
		}
		lic := LicenseOf(&p, "")
		if lic != "NOASSERTION" && lic != "" {
			c.Licenses = []cdxLic{{License: cdxLicID{ID: lic}}}
		}
		comps[i] = c
	}

	// Build dependency graph.
	deps := make([]cdxDep, len(r.Packages))
	for i, p := range r.Packages {
		var depRefs []string
		for _, group := range append(p.Depends, p.PreDepends...) {
			for _, alt := range group.Alts {
				depRefs = append(depRefs, bomRef(alt.Name))
				break // first alt
			}
		}
		deps[i] = cdxDep{Ref: bomRef(p.Name), DependsOn: depRefs}
	}

	doc := cdxDoc{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.5",
		SerialNumber: "urn:uuid:" + docNamespaceHash(r.Packages),
		Version:      1,
		Metadata: cdxMetadata{
			Timestamp: timestamp,
			Tools:     []cdxTool{{Name: "apt2distroless", Version: "dev"}},
		},
		Components:   comps,
		Dependencies: deps,
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal CycloneDX: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write CycloneDX %s: %w", path, err)
	}
	return nil
}
