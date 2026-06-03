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

// spdxDoc mirrors the SPDX 2.3 JSON structure we need. We build it manually
// rather than via tools-golang to avoid tight coupling to its internal model.
type spdxDoc struct {
	SPDXVersion       string       `json:"spdxVersion"`
	DataLicense       string       `json:"dataLicense"`
	SPDXID            string       `json:"SPDXID"`
	Name              string       `json:"name"`
	DocumentNamespace string       `json:"documentNamespace"`
	CreationInfo      spdxCreation `json:"creationInfo"`
	Packages          []spdxPkg    `json:"packages"`
	Relationships     []spdxRel    `json:"relationships"`
}

type spdxCreation struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPkg struct {
	SPDXID           string       `json:"SPDXID"`
	Name             string       `json:"name"`
	Version          string       `json:"versionInfo"`
	Supplier         string       `json:"supplier,omitempty"`
	ExternalRefs     []spdxExtRef `json:"externalRefs,omitempty"`
	LicenseConcluded string       `json:"licenseConcluded"`
	LicenseDeclared  string       `json:"licenseDeclared"`
	FilesAnalyzed    bool         `json:"filesAnalyzed"`
}

type spdxExtRef struct {
	Category string `json:"referenceCategory"`
	Type     string `json:"referenceType"`
	Locator  string `json:"referenceLocator"`
}

type spdxRel struct {
	SpdxElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSpdxElement string `json:"relatedSpdxElement"`
}

// WriteSPDX emits an SPDX 2.3 JSON document for the run result.
// licenses maps package name → SPDX license ID (pre-computed once by the caller).
func WriteSPDX(path string, r *manifest.RunResult, d Distro, epoch int64, licenses map[string]string) error {
	created := time.Unix(epoch, 0).UTC().Format(time.RFC3339)

	// Build stable SPDX ID per package: "SPDXRef-<name>".
	spdxID := func(name string) string {
		return "SPDXRef-" + strings.ReplaceAll(name, ":", "-")
	}

	pkgs := make([]spdxPkg, len(r.Packages))
	for i, p := range r.Packages {
		lic := ""
		if licenses != nil {
			lic = licenses[p.Name]
		}
		if lic == "" {
			lic = "NOASSERTION"
		}
		supplier := ""
		if p.Maintainer != "" {
			supplier = "Organization: " + p.Maintainer
		}
		pkgs[i] = spdxPkg{
			SPDXID:   spdxID(p.Name),
			Name:     p.Name,
			Version:  p.Version,
			Supplier: supplier,
			ExternalRefs: []spdxExtRef{{
				Category: "PACKAGE-MANAGER",
				Type:     "purl",
				Locator:  PURL(&p, d),
			}},
			LicenseConcluded: lic,
			LicenseDeclared:  lic,
			FilesAnalyzed:    false,
		}
	}

	// Build relationships from Depends/Pre-Depends edges.
	var rels []spdxRel
	// Root→package DESCRIBES relationships.
	for _, p := range r.Packages {
		rels = append(rels, spdxRel{
			SpdxElementID:      "SPDXRef-DOCUMENT",
			RelationshipType:   "DESCRIBES",
			RelatedSpdxElement: spdxID(p.Name),
		})
	}
	// DEPENDS_ON relationships from parsed Depends edges.
	for _, p := range r.Packages {
		for _, group := range append(p.Depends, p.PreDepends...) {
			for _, alt := range group.Alts {
				rels = append(rels, spdxRel{
					SpdxElementID:      spdxID(p.Name),
					RelationshipType:   "DEPENDS_ON",
					RelatedSpdxElement: spdxID(alt.Name),
				})
				break // first alt only for SBOM (matches what apt resolved)
			}
		}
	}

	doc := spdxDoc{
		SPDXVersion: "SPDX-2.3",
		DataLicense: "CC0-1.0",
		SPDXID:      "SPDXRef-DOCUMENT",
		Name:        strings.Join(r.Roots, "+"),
		DocumentNamespace: fmt.Sprintf(
			"https://spdx.org/spdxdocs/%s-%s",
			strings.Join(r.Roots, "-"),
			docNamespaceHash(r.Packages),
		),
		CreationInfo: spdxCreation{
			Created:  created,
			Creators: []string{"Tool: apt2distroless"},
		},
		Packages:      pkgs,
		Relationships: rels,
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal SPDX: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write SPDX %s: %w", path, err)
	}
	return nil
}
