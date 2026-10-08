package tui

import (
	"net"
	"strings"

	"verge-tui/internal/app"
	"verge-tui/internal/controller"
)

var connectionColumns = []struct {
	Name  string
	Width int
}{
	{"Host", 28}, {"DL Speed", 16}, {"UL Speed", 16}, {"Chains", 28}, {"Rule", 28},
	{"Process", 22}, {"Time", 21}, {"Source", 24}, {"Destination", 24}, {"Type", 18},
}

func connectionField(c controller.Connection, field string) string {
	switch field {
	case "Host":
		return connHost(c)
	case "DL Speed":
		return bytesString(c.DownloadSpeed) + "/s"
	case "UL Speed":
		return bytesString(c.UploadSpeed) + "/s"
	case "Chains":
		return connChains(c)
	case "Rule":
		return connRule(c)
	case "Process":
		if c.Metadata.Process != "" {
			return c.Metadata.Process
		}
		return c.Metadata.ProcessPath
	case "Time":
		return c.Start.Local().Format("2006-01-02 15:04:05")
	case "Source":
		return net.JoinHostPort(c.Metadata.SourceIP, c.Metadata.SourcePort)
	case "Destination":
		host := c.Metadata.DestinationIP
		if host == "" {
			host = c.Metadata.RemoteDestination
		}
		return net.JoinHostPort(host, c.Metadata.DestinationPort)
	case "Type":
		return c.Metadata.Type + "(" + c.Metadata.Network + ")"
	}
	return ""
}
func (m *Model) columnEnabled(name string) bool {
	if m.HiddenColumns == nil {
		return true
	}
	return !m.HiddenColumns[name]
}
func (m *Model) tableHeader(f *Frame, x, y, width int) {
	size := 0
	for _, c := range connectionColumns {
		if m.columnEnabled(c.Name) {
			size += c.Width
		}
	}
	m.TableLeft = max(0, min(max(0, size-width), m.TableLeft))
	colX := x - m.TableLeft
	for _, c := range connectionColumns {
		if !m.columnEnabled(c.Name) {
			continue
		}
		label := m.tr(c.Name)
		if m.TableSort == c.Name {
			if m.TableDescending {
				label += " ↓"
			} else {
				label += " ↑"
			}
		}
		f.Line(colX+1, y, c.Width-2, label, f.Theme.Card.Foreground(f.Theme.Muted).Bold(true))
		f.Add(Rect{colX, y, c.Width, 1}, app.Action{Kind: "table-sort", ID: c.Name})
		colX += c.Width
	}
}
func (m *Model) tableRow(f *Frame, x, y int, c controller.Connection) {
	r := Rect{x, y, f.Clip.W, 1}
	style := f.Theme.Card
	a := app.Action{Kind: "connection-detail", ID: c.ID}
	if m.Focus == Key(a) {
		style = style.Reverse(true)
	}
	f.Fill(r, style)
	f.Add(r, a)
	colX := x - m.TableLeft
	for _, column := range connectionColumns {
		if !m.columnEnabled(column.Name) {
			continue
		}
		f.Line(colX+1, y, column.Width-2, connectionField(c, column.Name), style)
		colX += column.Width
	}
}
func (m *Model) tableColumnDialog() {
	m.Modal = &Modal{Title: "Columns", List: true}
	for _, c := range connectionColumns {
		label := m.tr(c.Name)
		if m.columnEnabled(c.Name) {
			label = "● " + label
		} else {
			label = "○ " + label
		}
		m.Modal.Choices = append(m.Modal.Choices, Choice{label, app.Action{Kind: "table-column", ID: c.Name}})
	}
	m.Focus = ""
}
func (m *Model) connectionLess(a, b controller.Connection) bool {
	if m.ConnectionTable && m.TableSort != "" {
		less, equal := false, false
		switch m.TableSort {
		case "DL Speed":
			less, equal = a.DownloadSpeed < b.DownloadSpeed, a.DownloadSpeed == b.DownloadSpeed
		case "UL Speed":
			less, equal = a.UploadSpeed < b.UploadSpeed, a.UploadSpeed == b.UploadSpeed
		case "Time":
			less, equal = a.Start.Before(b.Start), a.Start.Equal(b.Start)
		default:
			left, right := strings.ToLower(connectionField(a, m.TableSort)), strings.ToLower(connectionField(b, m.TableSort))
			less, equal = left < right, left == right
		}
		if !equal {
			if m.TableDescending {
				return !less
			}
			return less
		}
	} else {
		if m.ConnectionOrder == "Upload Speed" && a.UploadSpeed != b.UploadSpeed {
			return a.UploadSpeed > b.UploadSpeed
		}
		if m.ConnectionOrder == "Download Speed" && a.DownloadSpeed != b.DownloadSpeed {
			return a.DownloadSpeed > b.DownloadSpeed
		}
	}
	if !a.Start.Equal(b.Start) {
		return a.Start.After(b.Start)
	}
	return a.ID < b.ID
}
