package libvirt

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"time"

	"omv-vmmanager/internal/models"

	"github.com/libvirt/libvirt-go"
)

// vmmanager metadata XML namespace & root. Stored inside the libvirt
// domain's <metadata> element. We use the *raw* XML form (not
// namespace-qualified Go types) so the structure is easy to inspect
// with `virsh dumpxml`.
//
// The namespace URL is kept stable across renames so VMs that were
// created under the original `webvm` namespace continue to parse.
// Changing the URL would orphan metadata on every existing VM.
const (
	vmmanagerNamespace = "https://webvm.local/ns"
	vmmanagerPrefix    = "vmmanager"
)

func nowUnix() int64 { return time.Now().Unix() }

// xmlMetaRoot is the wrapper element we write into <metadata>. Note that
// libvirt expects a single root child; we keep <meta> as the WebVM root
// and any future per-feature elements go inside it.
type xmlMetaRoot struct {
	XMLName   xml.Name   `xml:"meta"`
	XMLNS     string     `xml:"xmlns,attr"`
	Alias     string     `xml:"alias,omitempty"`
	Notes     string     `xml:"notes,omitempty"`
	Cover     string     `xml:"cover,omitempty"`
	Groups    []string   `xml:"groups>group,omitempty"`
	UpdatedAt int64      `xml:"updated_at,omitempty"`
}

// GetVMMeta fetches and parses the <metadata><webvm:meta> element of a
// domain. Returns an empty VMMeta (not an error) if the domain has no
// WebVM metadata yet.
func (c *Connector) GetVMMeta(uuid string) (models.VMMeta, error) {
	dom, err := c.lookupDomain(uuid)
	if err != nil {
		return models.VMMeta{}, err
	}
	defer dom.Free()

	raw, err := dom.GetMetadata(libvirt.DOMAIN_METADATA_ELEMENT, vmmanagerNamespace, libvirt.DOMAIN_AFFECT_CONFIG)
	if err != nil {
		// No metadata yet → empty.
		return models.VMMeta{}, nil
	}
	if raw == "" {
		return models.VMMeta{}, nil
	}

	var root xmlMetaRoot
	dec := xml.NewDecoder(bytes.NewReader([]byte(raw)))
	if err := dec.Decode(&root); err != nil {
		return models.VMMeta{}, fmt.Errorf("parse webvm metadata: %w", err)
	}
	return models.VMMeta{
		Alias:     root.Alias,
		Notes:     root.Notes,
		Cover:     root.Cover,
		Groups:    root.Groups,
		UpdatedAt: root.UpdatedAt,
	}, nil
}

// SetVMMeta writes the VMMeta to the domain's <metadata> element,
// replacing any previous WebVM metadata (read-modify-write keeps
// non-WebVM metadata intact via libvirt's namespace handling).
func (c *Connector) SetVMMeta(uuid string, meta models.VMMeta) error {
	dom, err := c.lookupDomain(uuid)
	if err != nil {
		return err
	}
	defer dom.Free()

	root := xmlMetaRoot{
		XMLNS:     vmmanagerNamespace,
		Alias:     meta.Alias,
		Notes:     meta.Notes,
		Cover:     meta.Cover,
		Groups:    meta.Groups,
		UpdatedAt: meta.UpdatedAt,
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("encode webvm metadata: %w", err)
	}

	xmlStr := buf.String()
	// SetMetadata expects a namespace URI as key and the XML as the value.
	return dom.SetMetadata(libvirt.DOMAIN_METADATA_ELEMENT, xmlStr, vmmanagerPrefix, vmmanagerNamespace, libvirt.DOMAIN_AFFECT_CONFIG)
}

// UpdateVMMeta reads the current metadata, applies the given partial
// update, and writes it back. nil pointers in the update mean "leave
// unchanged"; nil groups means "clear the list".
func (c *Connector) UpdateVMMeta(uuid string, upd models.VMMetaUpdate) (models.VMMeta, error) {
	current, err := c.GetVMMeta(uuid)
	if err != nil {
		return models.VMMeta{}, err
	}
	if upd.Alias != nil {
		current.Alias = *upd.Alias
	}
	if upd.Notes != nil {
		current.Notes = *upd.Notes
	}
	if upd.Cover != nil {
		current.Cover = *upd.Cover
	}
	if upd.Groups != nil {
		if *upd.Groups == nil {
			current.Groups = nil
		} else {
			current.Groups = *upd.Groups
		}
	}
	current.UpdatedAt = nowUnix()
	if err := c.SetVMMeta(uuid, current); err != nil {
		return models.VMMeta{}, err
	}
	return current, nil
}
