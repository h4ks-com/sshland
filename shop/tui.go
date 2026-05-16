package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateLoading state = iota
	stateBrowse
	stateView
	stateCreatingCheckout
	stateCheckout
	stateDone
	stateError
)

type articlesLoadedMsg struct{ articles []ArticleSummary }
type articleDetailMsg struct{ article *ArticleDetail }
type checkoutCreatedMsg struct{ resp *CheckoutCreateResponse }
type checkoutStatusMsg struct{ status *CheckoutStatusResponse }
type errMsg struct{ err error }

type model struct {
	client    *ShopClient
	publicURL string

	state     state
	statusMsg string
	errMsg    string
	width     int

	spin spinner.Model

	articles  []ArticleSummary
	browseIdx int

	current  *ArticleDetail
	colorIdx int
	sizeIdx  int
	rowIdx   int // 0 = color row, 1 = size row

	checkout  *CheckoutCreateResponse
	pollTicks int
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5c9eff"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0c8"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#7b8ab0"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e05"))
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Italic(true)
	priceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c4b"))
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#5c9eff")).Bold(true)
)

func newModel(client *ShopClient, publicURL string) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#5c9eff"))
	return model{
		client:    client,
		publicURL: publicURL,
		state:     stateLoading,
		spin:      sp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.loadArticles())
}

func (m model) loadArticles() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		page, err := m.client.ListArticles(ctx, 50)
		if err != nil {
			return errMsg{err}
		}
		return articlesLoadedMsg{page.Items}
	}
}

func (m model) loadDetail(id int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		a, err := m.client.GetArticle(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		return articleDetailMsg{a}
	}
}

func (m model) createCheckout() tea.Cmd {
	colors, _ := m.colorsAndSizes()
	colorVariants := m.variantsForColor(colors[m.colorIdx])
	v := colorVariants[m.sizeIdx]
	id := m.current.ID
	sku := v.SKU
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := m.client.CreateCheckout(ctx, id, sku, 1)
		if err != nil {
			return errMsg{err}
		}
		return checkoutCreatedMsg{resp}
	}
}

func (m model) pollCheckout() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := m.client.GetCheckoutStatus(ctx, m.checkout.SessionID)
		if err != nil {
			return errMsg{err}
		}
		return checkoutStatusMsg{st}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case errMsg:
		m.errMsg = msg.err.Error()
		m.state = stateError
		return m, nil
	case articlesLoadedMsg:
		m.articles = msg.articles
		m.state = stateBrowse
		return m, nil
	case articleDetailMsg:
		m.current = msg.article
		m.colorIdx = 0
		m.sizeIdx = 0
		m.rowIdx = 0
		m.state = stateView
		return m, nil
	case checkoutCreatedMsg:
		m.checkout = msg.resp
		m.state = stateCheckout
		return m, m.pollCheckout()
	case checkoutStatusMsg:
		switch msg.status.Status {
		case "complete":
			m.state = stateDone
			return m, nil
		case "expired":
			m.errMsg = "stripe session expired"
			m.state = stateError
			return m, nil
		}
		m.pollTicks++
		if m.pollTicks > 300 {
			m.errMsg = "timed out waiting for payment"
			m.state = stateError
			return m, nil
		}
		return m, m.pollCheckout()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.state {
	case stateBrowse:
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.browseIdx < len(m.articles)-1 {
				m.browseIdx++
			}
		case "k", "up":
			if m.browseIdx > 0 {
				m.browseIdx--
			}
		case "enter", " ":
			if len(m.articles) == 0 {
				return m, nil
			}
			m.state = stateLoading
			m.statusMsg = "loading product..."
			return m, m.loadDetail(m.articles[m.browseIdx].ID)
		}
	case stateView:
		colors, _ := m.colorsAndSizes()
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.state = stateBrowse
		case "j", "down":
			if m.rowIdx < 1 {
				m.rowIdx++
			}
		case "k", "up":
			if m.rowIdx > 0 {
				m.rowIdx--
			}
		case "tab", "shift+tab":
			m.rowIdx = 1 - m.rowIdx
		case "h", "left":
			shiftActiveRow(&m, -1, colors)
		case "l", "right":
			shiftActiveRow(&m, +1, colors)
		case "enter", " ":
			m.state = stateCreatingCheckout
			return m, m.createCheckout()
		}
	case stateCheckout:
		if k == "q" || k == "ctrl+c" {
			return m, tea.Quit
		}
	case stateDone:
		return m, tea.Quit
	case stateError:
		if k == "esc" {
			m.state = stateBrowse
			m.errMsg = ""
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	header := titleStyle.Render("h4ks/shop") + dimStyle.Render(" — support h4ks, get some merch")
	switch m.state {
	case stateLoading:
		msg := "loading shop..."
		if m.statusMsg != "" {
			msg = m.statusMsg
		}
		return header + "\n\n" + m.spin.View() + " " + msg + "\n"
	case stateCreatingCheckout:
		return header + "\n\n" + m.spin.View() + " creating checkout session...\n"
	case stateBrowse:
		return header + "\n\n" + m.viewBrowse()
	case stateView:
		return header + "\n\n" + m.viewArticle()
	case stateCheckout:
		return header + "\n\n" + m.viewCheckout()
	case stateDone:
		return header + "\n\n" + m.viewDone()
	case stateError:
		return header + "\n\n" + errStyle.Render("error: "+m.errMsg) + "\n\n" +
			hintStyle.Render("[esc] back to browse  [enter/q] quit")
	}
	return ""
}

func (m model) viewBrowse() string {
	if len(m.articles) == 0 {
		return dimStyle.Render("no products published yet — come back soon") + "\n\n" +
			hintStyle.Render("[q] quit")
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("%d items", len(m.articles))) + "\n\n")
	for i, a := range m.articles {
		cursor := "  "
		price := ""
		if a.PriceFrom > 0 {
			price = priceStyle.Render(fmt.Sprintf("from $%.2f", a.PriceFrom))
		}
		line := fmt.Sprintf("%s  %s", a.Title, price)
		if i == m.browseIdx {
			cursor = selStyle.Render("→ ")
			line = selStyle.Render(a.Title) + "  " + price
		}
		b.WriteString(cursor + line + "\n")
	}
	b.WriteString("\n" + hintStyle.Render("[j/k] move  [enter] details  [q] quit"))
	return b.String()
}

func (m model) viewArticle() string {
	a := m.current
	colors, _ := m.colorsAndSizes()
	if len(colors) == 0 {
		return errStyle.Render("sold out — check back later")
	}
	color := colors[m.colorIdx]
	sizes := m.variantsForColor(color)
	if m.sizeIdx >= len(sizes) {
		m.sizeIdx = 0
	}

	var b strings.Builder
	b.WriteString(headerStyle.Render(a.Title) + "\n")
	if a.Description != "" {
		b.WriteString(renderDescription(a.Description) + "\n")
	}
	colorMark := " "
	sizeMark := " "
	if m.rowIdx == 0 {
		colorMark = selStyle.Render("▸")
	} else {
		sizeMark = selStyle.Render("▸")
	}
	b.WriteString("\n" + colorMark + " " + headerStyle.Render("color:") + "  ")
	for i, c := range colors {
		if i == m.colorIdx {
			b.WriteString(selStyle.Render("[" + c + "]"))
		} else {
			b.WriteString(dimStyle.Render(" " + c + " "))
		}
		b.WriteString(" ")
	}
	b.WriteString("\n\n" + sizeMark + " " + headerStyle.Render("size:") + "  ")
	for i, v := range sizes {
		if i == m.sizeIdx {
			b.WriteString(selStyle.Render("[" + v.SizeName + "]"))
		} else {
			b.WriteString(dimStyle.Render(" " + v.SizeName + " "))
		}
		b.WriteString(" ")
	}
	if len(sizes) > 0 {
		v := sizes[m.sizeIdx]
		b.WriteString("\n\n" + priceStyle.Render(fmt.Sprintf("$%.2f", v.Price)) +
			dimStyle.Render(" + flat shipping at checkout"))
	}
	productURL := fmt.Sprintf("%s/p/%d", strings.TrimRight(m.publicURL, "/"), a.ID)
	b.WriteString("\n\n" + dimStyle.Render("photos & full description: ") +
		osc8Link(productURL, productURL))
	b.WriteString("\n\n" + hintStyle.Render("[↑/↓ j/k tab] row  [←/→ h/l] change  [enter] buy  [esc] back  [q] quit"))
	return b.String()
}

func (m model) viewCheckout() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("scan to pay:") + "\n\n")
	b.WriteString(renderQR(m.checkout.URL))

	w := m.width
	if w < 72 {
		w = 72
	}
	b.WriteString("\n" + dimStyle.Render("or open: ") + "\n")
	b.WriteString(osc8Link(m.checkout.URL, chunkURL(m.checkout.URL, w)) + "\n\n")
	b.WriteString(dimStyle.Render("ref: ") + m.checkout.ExternalOrderReference + "\n\n")
	b.WriteString(m.spin.View() + " " + dimStyle.Render("waiting for payment...") + "\n")
	b.WriteString(hintStyle.Render("[q] cancel"))
	return b.String()
}

// Terminals without OSC 8 support strip the escapes and show plain text.
func osc8Link(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// Soft-wrap confuses BubbleTea's height tracking and leaves ghost lines;
// hard-wrap with explicit \n instead.
func chunkURL(s string, width int) string {
	if len(s) <= width {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && i%width == 0 {
			b.WriteByte('\n')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (m model) viewDone() string {
	return headerStyle.Render("✓ payment received — thank you!") + "\n\n" +
		fmt.Sprintf("Ref: %s\n", m.checkout.ExternalOrderReference) +
		dimStyle.Render("you'll get a shipping email when it's on its way") + "\n\n" +
		hintStyle.Render("[any key] done")
}

func (m model) colorsAndSizes() ([]string, []string) {
	if m.current == nil {
		return nil, nil
	}
	colorSeen := map[string]bool{}
	sizeSeen := map[string]bool{}
	var colors, sizes []string
	for _, v := range m.current.Variants {
		if v.Stock <= 0 {
			continue
		}
		if !colorSeen[v.AppearanceName] && v.AppearanceName != "" {
			colorSeen[v.AppearanceName] = true
			colors = append(colors, v.AppearanceName)
		}
		if !sizeSeen[v.SizeName] && v.SizeName != "" {
			sizeSeen[v.SizeName] = true
			sizes = append(sizes, v.SizeName)
		}
	}
	return colors, sizes
}

func shiftActiveRow(m *model, dir int, colors []string) {
	if m.rowIdx == 0 {
		next := m.colorIdx + dir
		if next >= 0 && next < len(colors) {
			m.colorIdx = next
			m.sizeIdx = 0
		}
		return
	}
	sizes := m.variantsForColor(colors[m.colorIdx])
	next := m.sizeIdx + dir
	if next >= 0 && next < len(sizes) {
		m.sizeIdx = next
	}
}

func (m model) variantsForColor(color string) []ArticleVariantDTO {
	if m.current == nil {
		return nil
	}
	out := make([]ArticleVariantDTO, 0)
	for _, v := range m.current.Variants {
		if v.AppearanceName == color && v.Stock > 0 {
			out = append(out, v)
		}
	}
	return out
}

func renderDescription(html string) string {
	md, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return html
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(68),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimSpace(out)
}
