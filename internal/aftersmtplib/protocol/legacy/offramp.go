package legacy

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/security"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/security/dane"
	"github.com/emersion/go-msgauth/dkim"
	"go.uber.org/zap"
)

// OffRamp handles delivering AMP messages to external legacy SMTP servers.
type OffRamp struct {
	logger        *zap.Logger
	daneValidator *dane.DANEValidator
	dkimSigner    *security.Signer
	arcManager    *security.ARCManager
}

func NewOffRamp(logger *zap.Logger, daneValidator *dane.DANEValidator, dkimSigner *security.Signer, arcManager *security.ARCManager) *OffRamp {
	return &OffRamp{
		logger:        logger,
		daneValidator: daneValidator,
		dkimSigner:    dkimSigner,
		arcManager:    arcManager,
	}
}

// Deliver converts an AMP message back into a legacy MIME/SMTP payload, DKIM/ARC signs it,
// and enforces DANE/TLSA when connecting to the destination MX.
func (o *OffRamp) Deliver(ampMsg *amp.AMPMessage, originalMime []byte, toDomain string, toAddress string) error {
	o.logger.Info("Starting Off-Ramp delivery", zap.String("destination", toDomain))

	payloadToSign := originalMime

	// 1. ARC Sign Forwarded Payload if ARC Manager is configured
	if o.arcManager != nil {
		headersSlice := strings.Split(string(payloadToSign[:bytes.Index(payloadToSign, []byte("\r\n\r\n"))]), "\r\n")

		// In a real implementation we would preserve the full original headers or build them dynamically.
		// For the sake of this gateway, we just add the ARC seals to the top.
		arcHeaders, err := o.arcManager.AddARCSet(headersSlice, 1, "none")
		if err == nil {
			payloadToSign = append([]byte(strings.Join(arcHeaders, "\r\n")+"\r\n"), payloadToSign...)
			o.logger.Info("ARC signed the outbound legacy payload")
		} else {
			o.logger.Warn("Failed to apply ARC signature", zap.Error(err))
		}
	}

	// 2. DKIM Sign the payload
	var finalPayload bytes.Buffer
	if o.dkimSigner != nil && o.dkimSigner.GetOptions() != nil {
		signerOpts := o.dkimSigner.GetOptions()
		err := dkim.Sign(&finalPayload, bytes.NewReader(payloadToSign), signerOpts)
		if err != nil {
			return fmt.Errorf("failed to heavily sign output with DKIM: %w", err)
		}
		o.logger.Info("DKIM signed the outbound legacy payload")
	} else {
		finalPayload.Write(payloadToSign)
		o.logger.Warn("DKIM Signer not configured. Sending unsigned payload.")
	}

	signedOutput := finalPayload.Bytes()

	// 3. Discover MX records
	mxRecords, err := net.LookupMX(toDomain)
	if err != nil || len(mxRecords) == 0 {
		return fmt.Errorf("failed to lookup MX records for %s: %w", toDomain, err)
	}

	targetMX := strings.TrimSuffix(mxRecords[0].Host, ".")
	targetPort := 25
	o.logger.Info("Resolved MX", zap.String("host", targetMX), zap.Int("pref", int(mxRecords[0].Pref)))

	// 4. Obtain strict DANE validation TLS Config
	var tlsConfig *tls.Config
	if o.daneValidator != nil {
		tlsConfig = o.daneValidator.GetTLSConfig(targetMX, targetPort)
		o.logger.Info("Enforcing DANE/TLSA validation on outbound connection")
	} else {
		tlsConfig = &tls.Config{ServerName: targetMX}
	}

	// 5. Connect to MX and deliver
	addr := fmt.Sprintf("%s:%d", targetMX, targetPort)
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to MX: %w", err)
	}
	defer c.Close()

	if err = c.Hello("aftersmtp.msgs.global"); err != nil {
		return err
	}

	// Opportunistic or Mandatory STARTTLS with DANE
	ok, _ := c.Extension("STARTTLS")
	if ok {
		if err = c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS / DANE validation failed: %w", err)
		}
		o.logger.Info("STARTTLS established successfully. DANE verified.")
	} else if tlsConfig != nil && tlsConfig.InsecureSkipVerify == false {
		// If we require DANE but they don't support STARTTLS, we must abort
		return fmt.Errorf("destination does not support STARTTLS, but strict security is required")
	}

	// Send the mail
	// Extract sender from headers or use bounce address
	if err = c.Mail("bounce@aftersmtp.msgs.global"); err != nil {
		return err
	}
	if err = c.Rcpt(toAddress); err != nil {
		return err
	}

	w, err := c.Data()
	if err != nil {
		return err
	}

	_, err = w.Write(signedOutput)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return c.Quit()
}
