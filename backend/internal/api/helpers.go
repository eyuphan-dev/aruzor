package api

import (
	"errors"
	"strconv"
	"time"
)

var (
	errMissingQuery              = errors.New("query parametresi gerekli")
	errMissingToken              = errors.New("yetkilendirme tokeni gerekli")
	errInvalidBody               = errors.New("istek govdesi gecersiz")
	errInvalidCredentials        = errors.New("e-posta veya sifre hatali")
	errInternal                  = errors.New("beklenmeyen bir hata olustu")
	errForbidden                 = errors.New("bu islem icin yetkiniz yok")
	errMissingUserID             = errors.New("userId parametresi gerekli")
	errInvalidScope              = errors.New("scope 'all' veya 'user' olmali")
	errInvalidOperator           = errors.New("operator '>', '>=', '<', '<=' veya '==' olmali")
	errWeakCredentials           = errors.New("e-posta gerekli ve sifre en az 8 karakter olmali")
	errInvalidRole               = errors.New("gecersiz rol")
	errUserExists                = errors.New("bu e-posta ile zaten bir kullanici var")
	errSelfDelete                = errors.New("kendi hesabini silemezsin")
	errTooManyAttempts           = errors.New("cok fazla giris denemesi, lutfen biraz sonra tekrar dene")
	errDashboardNotFound         = errors.New("dashboard bulunamadi")
	errDatasourceNotFound        = errors.New("veri kaynagi bulunamadi")
	errCannotDeleteDefault       = errors.New("varsayilan veri kaynagi silinemez")
	errUpstreamUnavailable       = errors.New("veri kaynagina ulasilamadi")
	errUnknownSetting            = errors.New("bilinmeyen ayar anahtari")
	errCannotDeleteLastDashboard = errors.New("son dashboard silinemez")
	errInvalidMonitorType        = errors.New("tur 'http' veya 'tcp' olmali")
	errStatusPageDisabled        = errors.New("durum sayfasi bulunamadi")
	errMonitorNotFound           = errors.New("izleme bulunamadi")
)

func parseUnixTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, errors.New("gecersiz zaman formati: " + v)
	}
	return time.Unix(sec, 0), nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
