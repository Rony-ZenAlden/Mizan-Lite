package api

import (
	"context"
	"encoding/base64"
	"os"
	"strconv"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// Store information (the owner's request, 2026-09-24): the name, phone, city and address at the head of every invoice and
// receipt, and the shop's own logo. Nothing about any particular shop is in the application — each shop sets its own
// here, and with no logo the documents set the name in type.

// Acts in the owner's history: what heads every document is the owner's to change.
const (
	ActShopInfo = "settings.shop_info.set"
	ActShopLogo = "settings.shop_logo.set"
)

// ShopDTO is the store information and the logo as the documents print them.
type ShopDTO struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	City    string `json:"city"`
	Address string `json:"address"`
	// Logo is the stored logo as base64 PNG, "" when there is none.
	Logo       string `json:"logo"`
	LogoWidth  int    `json:"logoWidth"`
	LogoHeight int    `json:"logoHeight"`
}

// ShopInput changes the store information.
type ShopInput struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	City    string `json:"city"`
	Address string `json:"address"`
}

// LogoFileDTO is the file the owner chose, or Cancelled.
type LogoFileDTO struct {
	Path      string `json:"path"`
	Cancelled bool   `json:"cancelled"`
}

func shopDTO(ctx context.Context, app *bootstrap.App) (ShopDTO, error) {
	current, err := app.Settings.Get(ctx)
	if err != nil {
		return ShopDTO{}, err
	}
	r := current.Receipt
	out := ShopDTO{Name: current.ShopName, Phone: r.Phone, City: r.City, Address: r.Address}
	logo, found, err := app.Settings.Logo(ctx)
	if err != nil || !found {
		return out, err
	}
	out.Logo, out.LogoWidth, out.LogoHeight = base64.StdEncoding.EncodeToString(logo.PNG), logo.Width, logo.Height
	return out, nil
}

// Shop reads the store information and the logo.
func (s *Settings) Shop() envelope.Result[ShopDTO] {
	return call(s.core, "Settings.Shop", shopDTO)
}

// SaveShop changes the name, phone, city and address — one guarded act, recorded with the name.
func (s *Settings) SaveShop(in ShopInput) envelope.Result[ShopDTO] {
	return call(s.core, "Settings.SaveShop", func(ctx context.Context, app *bootstrap.App) (ShopDTO, error) {
		err := app.DB.Do(ctx, func(ctx context.Context) error {
			next, err := app.Settings.Update(ctx, settingsdomain.Update{ShopName: &in.Name,
				Printing: settingsdomain.PrintingUpdate{Phone: &in.Phone, City: &in.City, Address: &in.Address}})
			if err != nil {
				return err
			}
			return requireOwner(ctx, app, ActShopInfo, next.ShopName)
		})
		if err != nil {
			return ShopDTO{}, err
		}
		return shopDTO(ctx, app)
	})
}

// PickLogoFile opens the file dialog for a logo and returns the file chosen. It changes nothing: SetLogo does, as a
// guarded act — so a PIN asked for at SetLogo retries SetLogo alone, not the dialog.
func (s *Settings) PickLogoFile() envelope.Result[LogoFileDTO] {
	return call(s.core, "Settings.PickLogoFile", func(ctx context.Context, app *bootstrap.App) (LogoFileDTO, error) {
		w, err := newWords(ctx, app)
		if err != nil {
			return LogoFileDTO{}, err
		}
		files, err := s.core.dialogs()
		if err != nil {
			return LogoFileDTO{}, err
		}
		path, err := files.OpenFile(ctx, w.t("settings.logo_choose"), []Filter{{Name: w.t("settings.logo_filter"), Pattern: "*.png;*.jpg;*.jpeg"}})
		if err != nil {
			return LogoFileDTO{}, err
		}
		return LogoFileDTO{Path: path, Cancelled: path == ""}, nil
	})
}

// SetLogo makes the picture at path the shop's logo: read, made the size every printout needs, and kept in the
// database so a backup carries it. A guarded act.
func (s *Settings) SetLogo(path string) envelope.Result[ShopDTO] {
	return call(s.core, "Settings.SetLogo", func(ctx context.Context, app *bootstrap.App) (ShopDTO, error) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return ShopDTO{}, errs.Validation(settingsdomain.CodeLogoInvalid, "the logo file cannot be read")
		}
		// Refused by its size before it is read: the wrong file chosen should not be read into memory to find out.
		if info.Size() > settingsdomain.MaxLogoBytes {
			return ShopDTO{}, errs.Validation(settingsdomain.CodeLogoTooLarge, "the logo file is too large").
				WithParam("max", strconv.Itoa(settingsdomain.MaxLogoBytes>>20))
		}
		raw, err := os.ReadFile(path) //nolint:gosec // a file the owner chose in the system's dialog
		if err != nil {
			return ShopDTO{}, errs.Validation(settingsdomain.CodeLogoInvalid, "the logo file cannot be read")
		}
		err = app.DB.Do(ctx, func(ctx context.Context) error {
			if _, err := app.Settings.SetLogo(ctx, raw); err != nil {
				return err
			}
			return requireOwner(ctx, app, ActShopLogo, info.Name())
		})
		if err != nil {
			return ShopDTO{}, err
		}
		return shopDTO(ctx, app)
	})
}

// RemoveLogo takes the logo away; the documents set the shop's name in type instead. A guarded act.
func (s *Settings) RemoveLogo() envelope.Result[ShopDTO] {
	return call(s.core, "Settings.RemoveLogo", func(ctx context.Context, app *bootstrap.App) (ShopDTO, error) {
		err := app.DB.Do(ctx, func(ctx context.Context) error {
			if err := app.Settings.RemoveLogo(ctx); err != nil {
				return err
			}
			return requireOwner(ctx, app, ActShopLogo, "")
		})
		if err != nil {
			return ShopDTO{}, err
		}
		return shopDTO(ctx, app)
	})
}
