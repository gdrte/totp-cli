//go:build gui

package gui

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/yitsushi/totp-cli/internal/security"
	"github.com/yitsushi/totp-cli/internal/server"
)

type (
	// C is shorthand for layout.Context.
	C = layout.Context
	// D is shorthand for layout.Dimensions.
	D = layout.Dimensions
)

const (
	// frameInterval keeps the countdown bars moving smoothly.
	frameInterval = 250 * time.Millisecond
	// lowRemaining turns a countdown red.
	lowRemaining    = 5 * time.Second
	lockCardWidth   = 380
	maxContentWidth = 860
)

var (
	colorBg     = color.NRGBA{R: 0xf3, G: 0xf4, B: 0xf6, A: 0xff}
	colorCard   = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colorBorder = color.NRGBA{R: 0xd1, G: 0xd5, B: 0xdb, A: 0xff}
	colorMuted  = color.NRGBA{R: 0x6b, G: 0x72, B: 0x80, A: 0xff}
	colorAccent = color.NRGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff}
	colorDanger = color.NRGBA{R: 0xdc, G: 0x26, B: 0x26, A: 0xff}
	colorOK     = color.NRGBA{R: 0x15, G: 0x80, B: 0x3d, A: 0xff}
	colorWarn   = color.NRGBA{R: 0xb4, G: 0x53, B: 0x09, A: 0xff}
	colorSubtle = color.NRGBA{R: 0xe5, G: 0xe7, B: 0xeb, A: 0xff}
)

func (u *ui) layout(gtx C) D {
	paint.Fill(gtx.Ops, colorBg)

	if !u.unlocked {
		return u.layoutLock(gtx)
	}

	u.handleMainEvents(gtx)

	// Accounts can also change through the API or `totp-cli serve`.
	if !gtx.Now.Before(u.nextReload) {
		u.reload()
	}

	for _, r := range u.rows {
		u.refreshCode(r, gtx.Now)
	}

	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(frameInterval)})

	return u.layoutMain(gtx)
}

// Lock screen.

func (u *ui) layoutLock(gtx C) D {
	for _, ed := range []*widget.Editor{&u.password, &u.confirm} {
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}

			if _, ok := ev.(widget.SubmitEvent); ok && !u.busy {
				u.submitPassword()
			}
		}
	}

	if u.unlockBtn.Clicked(gtx) && !u.busy {
		u.submitPassword()
	}

	title, action := "Unlock database", "Unlock"
	if !u.initialized {
		title, action = "Create database password", "Create"
	}

	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(lockCardWidth))
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		return card(gtx, func(gtx C) D {
			children := []layout.FlexChild{
				layout.Rigid(material.H6(u.th, title).Layout),
				layout.Rigid(u.muted(u.cfg.DBPath)),
				gap(12),
				layout.Rigid(func(gtx C) D { return u.field(gtx, "Password", &u.password, "", false) }),
			}

			if !u.initialized {
				children = append(
					children,
					gap(8),
					layout.Rigid(func(gtx C) D { return u.field(gtx, "Confirm password", &u.confirm, "", false) }),
					layout.Rigid(u.muted("There is no way to recover a forgotten password.")),
				)
			}

			children = append(
				children,
				gap(12),
				layout.Rigid(func(gtx C) D {
					if u.busy {
						gtx = gtx.Disabled()
					}

					return u.button(gtx, &u.unlockBtn, action, colorAccent)
				}),
				layout.Rigid(u.layoutStatus),
			)

			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// Main screen.

func (u *ui) handleMainEvents(gtx C) {
	if u.copyTok.Clicked(gtx) {
		copyText(gtx, u.apiToken)
		u.setStatus("API token copied.", false)
	}

	if u.registerBtn.Clicked(gtx) {
		u.register()
	}

	for _, r := range u.rows {
		clicked := r.copyCode.Clicked(gtx)
		if r.clickCode.Clicked(gtx) {
			clicked = true
		}

		if clicked && r.code != "" && r.code != codeError {
			copyText(gtx, r.code)
			u.setStatus(fmt.Sprintf("Copied code for %s/%s.", r.entry.Namespace, r.entry.Name), false)
		}

		if r.copyCurl.Clicked(gtx) {
			cmd := u.curlCommand(r.entry)
			copyText(gtx, cmd)
			u.setStatus("Copied: "+cmd, false)
		}

		if r.del.Clicked(gtx) {
			if r.armed(gtx.Now) {
				r.armedAt = time.Time{}
				u.deleteAccount(r.entry)
			} else {
				r.armedAt = gtx.Now
			}
		}
	}
}

func (u *ui) layoutMain(gtx C) D {
	items := []layout.Widget{u.layoutHeader, u.layoutStatus, u.layoutRegister, u.layoutAccountsHead}

	switch {
	case !u.loaded:
		items = append(items, u.muted("Loading…"))
	case len(u.rows) == 0:
		items = append(items, u.muted("No accounts yet."))
	}

	for _, r := range u.rows {
		items = append(items, func(gtx C) D { return u.layoutRow(gtx, r) })
	}

	return material.List(u.th, &u.list).Layout(gtx, len(items), func(gtx C, i int) D {
		return layout.Inset{Left: 16, Right: 16, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(maxContentWidth))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X

			return items[i](gtx)
		})
	})
}

func (u *ui) layoutHeader(gtx C) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(
		gtx,
		layout.Rigid(func(gtx C) D {
			title := material.H5(u.th, "totp-cli")
			title.Font.Weight = font.Bold

			return title.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(
				gtx,
				layout.Rigid(func(gtx C) D {
					if u.apiAddr == "" {
						return D{}
					}

					return layout.Flex{Alignment: layout.Middle}.Layout(
						gtx,
						layout.Rigid(func(gtx C) D {
							l := material.Body2(u.th, "API http://"+u.apiAddr)
							l.Font = u.mono

							return l.Layout(gtx)
						}),
						hgap(8),
						layout.Rigid(func(gtx C) D { return u.button(gtx, &u.copyTok, "Copy token", colorSubtle) }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if u.apiNote == "" {
						return D{}
					}

					l := material.Caption(u.th, u.apiNote)
					l.Color = colorWarn

					return l.Layout(gtx)
				}),
			)
		}),
	)
}

func (u *ui) layoutStatus(gtx C) D {
	if u.status == "" {
		return D{}
	}

	l := material.Body2(u.th, u.status)
	l.Color = colorOK

	if u.statusErr {
		l.Color = colorDanger
	}

	return layout.Inset{Top: 6}.Layout(gtx, l.Layout)
}

func (u *ui) layoutRegister(gtx C) D {
	return card(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(material.H6(u.th, "Register a site").Layout),
			gap(8),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{}.Layout(
					gtx,
					layout.Rigid(material.RadioButton(u.th, &u.mode, modeSecret, "Secret key").Layout),
					hgap(16),
					layout.Rigid(material.RadioButton(u.th, &u.mode, modeURI, "otpauth:// URI").Layout),
				)
			}),
			gap(8),
			layout.Rigid(func(gtx C) D {
				return row2(
					gtx,
					func(gtx C) D { return u.field(gtx, "Namespace", &u.namespace, "e.g. staging", false) },
					func(gtx C) D { return u.field(gtx, "Account", &u.account, "e.g. test-user@example.com", false) },
				)
			}),
			gap(8),
		}

		if u.mode.Value == modeURI {
			children = append(
				children,
				layout.Rigid(func(gtx C) D {
					return u.field(gtx, "otpauth URI", &u.uri, "otpauth://totp/Example:alice?secret=…&issuer=Example", true)
				}),
				layout.Rigid(u.muted("Namespace and account come from the URI's issuer and label when left blank.")),
			)
		} else {
			children = append(
				children,
				layout.Rigid(func(gtx C) D { return u.field(gtx, "Secret (base32)", &u.secret, "JBSWY3DPEHPK3PXP", true) }),
			)
		}

		children = append(
			children,
			gap(4),
			layout.Rigid(material.CheckBox(u.th, &u.advanced, "Advanced").Layout),
		)

		if u.advanced.Value {
			children = append(
				children,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(
						gtx,
						layout.Rigid(u.muted("Algorithm")),
						hgap(8),
						layout.Rigid(material.RadioButton(u.th, &u.algorithm, "", "default (sha1)").Layout),
						layout.Rigid(material.RadioButton(u.th, &u.algorithm, "sha1", "sha1").Layout),
						layout.Rigid(material.RadioButton(u.th, &u.algorithm, "sha256", "sha256").Layout),
						layout.Rigid(material.RadioButton(u.th, &u.algorithm, "sha512", "sha512").Layout),
					)
				}),
				gap(4),
				layout.Rigid(func(gtx C) D {
					return row2(
						gtx,
						func(gtx C) D {
							return row2(
								gtx,
								func(gtx C) D { return u.field(gtx, "Digits", &u.digits, "6", false) },
								func(gtx C) D { return u.field(gtx, "Period (s)", &u.period, "30", false) },
							)
						},
						func(gtx C) D { return u.field(gtx, "Prefix", &u.prefix, "", false) },
					)
				}),
			)
		}

		children = append(
			children,
			gap(12),
			layout.Rigid(func(gtx C) D { return u.button(gtx, &u.registerBtn, "Register", colorAccent) }),
		)

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (u *ui) layoutAccountsHead(gtx C) D {
	return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Baseline}.Layout(
			gtx,
			layout.Flexed(1, material.H6(u.th, "Accounts").Layout),
			layout.Rigid(u.muted("Click a code to copy it.")),
		)
	})
}

func (u *ui) layoutRow(gtx C, r *row) D {
	period := time.Duration(server.EffectivePeriod(r.entry.TimePeriod)) * time.Second
	left := max(r.expiresAt.Sub(gtx.Now), 0)

	return card(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(
			gtx,
			layout.Flexed(1, func(gtx C) D {
				meta := fmt.Sprintf("%s · %d digits · %ds",
					cmp.Or(r.entry.Algorithm, "sha1"), cmp.Or(r.entry.Length, security.DefaultLength), int64(period/time.Second))
				if r.entry.HasPrefix {
					meta += " · prefixed"
				}

				return layout.Flex{Axis: layout.Vertical}.Layout(
					gtx,
					layout.Rigid(func(gtx C) D {
						l := material.Body1(u.th, r.entry.Namespace+" / "+r.entry.Name)
						l.Font.Weight = font.SemiBold
						l.MaxLines = 1

						return l.Layout(gtx)
					}),
					layout.Rigid(u.muted(meta)),
				)
			}),
			hgap(12),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(
					gtx,
					layout.Rigid(func(gtx C) D {
						code := r.code
						if code == "" {
							code = "······"
						}

						return material.Clickable(gtx, &r.clickCode, func(gtx C) D {
							l := material.Label(u.th, unit.Sp(24), code)
							l.Font = u.mono
							l.Font.Weight = font.Bold

							if code == codeError {
								l.Color = colorDanger
							}

							return l.Layout(gtx)
						})
					}),
					gap(4),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Dp(140)
						gtx.Constraints.Max.X = gtx.Constraints.Min.X

						bar := material.ProgressBar(u.th, float32(left)/float32(period))
						bar.Height = unit.Dp(4)
						bar.TrackColor = colorSubtle

						if left < lowRemaining {
							bar.Color = colorDanger
						}

						return bar.Layout(gtx)
					}),
				)
			}),
			hgap(12),
			layout.Rigid(func(gtx C) D { return u.button(gtx, &r.copyCode, "Copy", colorSubtle) }),
			hgap(6),
			layout.Rigid(func(gtx C) D {
				if u.apiAddr == "" {
					return D{}
				}

				return u.button(gtx, &r.copyCurl, "curl", colorSubtle)
			}),
			hgap(6),
			layout.Rigid(func(gtx C) D {
				label := "Delete"
				if r.armed(gtx.Now) {
					label = "Confirm"
				}

				return u.button(gtx, &r.del, label, colorDanger)
			}),
		)
	})
}

func (r *row) armed(now time.Time) bool {
	return !r.armedAt.IsZero() && now.Sub(r.armedAt) < deleteConfirmWindow
}

// Widgets.

func (u *ui) field(gtx C, label string, ed *widget.Editor, hint string, mono bool) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(
		gtx,
		layout.Rigid(u.muted(label)),
		gap(2),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X

			return widget.Border{Color: colorBorder, CornerRadius: unit.Dp(6), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx C) D {
					e := material.Editor(u.th, ed, hint)
					if mono {
						e.Font = u.mono
					}

					return e.Layout(gtx)
				})
			})
		}),
	)
}

func (u *ui) button(gtx C, btn *widget.Clickable, label string, bg color.NRGBA) D {
	b := material.Button(u.th, btn, label)
	b.Background = bg
	b.TextSize = unit.Sp(13)
	b.CornerRadius = unit.Dp(6)
	b.Inset = layout.Inset{Top: 6, Bottom: 6, Left: 12, Right: 12}

	if bg == colorSubtle {
		b.Color = u.th.Fg
	}

	return b.Layout(gtx)
}

func (u *ui) muted(txt string) layout.Widget {
	return func(gtx C) D {
		l := material.Caption(u.th, txt)
		l.Color = colorMuted

		return l.Layout(gtx)
	}
}

// card draws w on a rounded, bordered panel spanning the available width.
func card(gtx C, w layout.Widget) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	return layout.Background{}.Layout(
		gtx,
		func(gtx C) D {
			rect := image.Rectangle{Max: gtx.Constraints.Min}
			defer clip.UniformRRect(rect, gtx.Dp(8)).Push(gtx.Ops).Pop()

			paint.Fill(gtx.Ops, colorCard)

			return D{Size: rect.Max}
		},
		func(gtx C) D {
			return widget.Border{Color: colorBorder, CornerRadius: unit.Dp(8), Width: unit.Dp(1)}.Layout(gtx, func(gtx C) D {
				return layout.UniformInset(unit.Dp(14)).Layout(gtx, w)
			})
		},
	)
}

// row2 lays out two widgets side by side with equal widths.
func row2(gtx C, left, right layout.Widget) D {
	return layout.Flex{}.Layout(
		gtx,
		layout.Flexed(1, left),
		hgap(10),
		layout.Flexed(1, right),
	)
}

func gap(dp unit.Dp) layout.FlexChild {
	return layout.Rigid(layout.Spacer{Height: dp}.Layout)
}

func hgap(dp unit.Dp) layout.FlexChild {
	return layout.Rigid(layout.Spacer{Width: dp}.Layout)
}

func copyText(gtx C, text string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(text))})
}
