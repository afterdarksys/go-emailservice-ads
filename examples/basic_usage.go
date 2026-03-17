package main

import (
	"fmt"
	"log"
	"os"

	"github.com/afterdarksys/go-emailservice-ads/msgfmt"
)

func main() {
	fmt.Println("=== ADS Mail Format (AMF) Examples ===\n")

	// Example 1: Create a simple message
	example1_SimpleMessage()

	// Example 2: Message with attachments
	example2_WithAttachments()

	// Example 3: Encryption
	example3_Encryption()

	// Example 4: Digital signatures
	example4_Signatures()

	// Example 5: Sign and encrypt
	example5_SignAndEncrypt()

	// Example 6: Convert from EML
	example6_ConvertEML()

	// Example 7: Extended message with AI metadata
	example7_ExtendedMessage()

	// Example 8: Reply and forward
	example8_ReplyForward()
}

func example1_SimpleMessage() {
	fmt.Println("Example 1: Simple Message")
	fmt.Println("---------------------------")

	// Create a new message
	msg := msgfmt.NewMessage(
		"alice@example.com",
		"bob@example.com",
		"Hello, World!",
	)

	// Set the body
	msg.SetBody("This is a simple email message using the AMF format.")

	// Add metadata
	msg.AddLabel("personal", "important")
	msg.AddTag("hello-world")
	msg.SetPriority(msgfmt.PriorityNormal)

	// Save to file
	writer := msgfmt.NewWriter(&msgfmt.WriterOptions{
		Indent: true,
	})

	file, _ := os.Create("/tmp/simple_message.amf")
	defer file.Close()

	if err := writer.Write(file, msg); err != nil {
		log.Fatalf("Failed to write message: %v", err)
	}

	fmt.Printf("✓ Message created: %s\n", msg.ID)
	fmt.Printf("✓ Saved to: /tmp/simple_message.amf\n")
	fmt.Printf("✓ Size: %d bytes\n\n", msg.Size())
}

func example2_WithAttachments() {
	fmt.Println("Example 2: Message with Attachments")
	fmt.Println("-------------------------------------")

	msg := msgfmt.NewMessage(
		"alice@example.com",
		"bob@example.com",
		"Project files attached",
	)

	msg.SetBody("Hi Bob,\n\nPlease find the project files attached.\n\nBest regards,\nAlice")

	// Add attachments
	msg.AddAttachment("document.txt", "text/plain", []byte("Document content here"))
	msg.AddAttachment("report.pdf", "application/pdf", []byte("PDF content here"))

	// Save
	writer := msgfmt.NewWriter(&msgfmt.WriterOptions{
		Indent:      true,
		Compression: msgfmt.CompressionGzip, // Use compression for larger files
	})

	file, _ := os.Create("/tmp/message_with_attachments.amfz")
	defer file.Close()

	writer.Write(file, msg)

	fmt.Printf("✓ Message with %d attachments created\n", len(msg.Attachments))
	fmt.Printf("✓ Saved as compressed file: /tmp/message_with_attachments.amfz\n\n")
}

func example3_Encryption() {
	fmt.Println("Example 3: Encrypted Message")
	fmt.Println("------------------------------")

	msg := msgfmt.NewMessage(
		"alice@example.com",
		"bob@example.com",
		"Secret message",
	)

	msg.SetBody("This is a confidential message that should be encrypted.")
	msg.AddLabel("confidential")

	// Generate encryption key
	key, err := msgfmt.GenerateKey()
	if err != nil {
		log.Fatalf("Failed to generate key: %v", err)
	}

	// Encrypt the message
	encMsg, err := msgfmt.EncryptAES256GCM(msg, key)
	if err != nil {
		log.Fatalf("Failed to encrypt: %v", err)
	}

	fmt.Printf("✓ Message encrypted with AES-256-GCM\n")
	fmt.Printf("✓ Original size: %d bytes\n", msg.Size())
	fmt.Printf("✓ Key (hex): %x\n\n", key)

	// Decrypt to verify
	decMsg, err := msgfmt.DecryptAES256GCM(encMsg, key)
	if err != nil {
		log.Fatalf("Failed to decrypt: %v", err)
	}

	fmt.Printf("✓ Message decrypted successfully\n")
	fmt.Printf("✓ Body: %s\n\n", decMsg.Body.Text)
}

func example4_Signatures() {
	fmt.Println("Example 4: Digital Signatures")
	fmt.Println("-------------------------------")

	msg := msgfmt.NewMessage(
		"alice@example.com",
		"bob@example.com",
		"Signed message",
	)

	msg.SetBody("This message is digitally signed for authenticity.")

	// Generate key pair (Ed25519 - fast and secure)
	signingKey, verifyingKey, err := msgfmt.GenerateEd25519KeyPair()
	if err != nil {
		log.Fatalf("Failed to generate keys: %v", err)
	}

	// Sign the message
	if err := msgfmt.SignMessage(msg, signingKey, "alice@example.com"); err != nil {
		log.Fatalf("Failed to sign: %v", err)
	}

	fmt.Printf("✓ Message signed with Ed25519\n")
	fmt.Printf("✓ Signer: %s\n", msg.Security.Signature.Signer)
	fmt.Printf("✓ Timestamp: %s\n", msg.Security.Signature.Timestamp)

	// Verify the signature
	valid, err := msgfmt.VerifySignature(msg, verifyingKey)
	if err != nil {
		log.Fatalf("Failed to verify: %v", err)
	}

	if valid {
		fmt.Printf("✓ Signature is VALID\n\n")
	} else {
		fmt.Printf("✗ Signature is INVALID\n\n")
	}
}

func example5_SignAndEncrypt() {
	fmt.Println("Example 5: Sign AND Encrypt")
	fmt.Println("-----------------------------")

	msg := msgfmt.NewMessage(
		"alice@example.com",
		"bob@example.com",
		"Top secret",
	)

	msg.SetBody("This message is both signed and encrypted for maximum security.")

	// Generate keys
	signingKey, verifyingKey, _ := msgfmt.GenerateEd25519KeyPair()
	encKey, _ := msgfmt.GenerateKey()

	// Sign and encrypt in one operation
	encMsg, err := msgfmt.SignAndEncrypt(msg, signingKey, "alice@example.com", encKey)
	if err != nil {
		log.Fatalf("Failed to sign and encrypt: %v", err)
	}

	fmt.Printf("✓ Message signed and encrypted\n")

	// Decrypt and verify
	decMsg, valid, err := msgfmt.DecryptAndVerify(encMsg, encKey, verifyingKey)
	if err != nil {
		log.Fatalf("Failed to decrypt and verify: %v", err)
	}

	fmt.Printf("✓ Message decrypted\n")
	fmt.Printf("✓ Signature valid: %v\n", valid)
	fmt.Printf("✓ Body: %s\n\n", decMsg.Body.Text)
}

func example6_ConvertEML() {
	fmt.Println("Example 6: Convert from .eml")
	fmt.Println("-----------------------------")

	emlData := `From: alice@example.com
To: bob@example.com
Subject: Converted from EML
Date: Mon, 09 Mar 2026 10:00:00 +0000
Message-ID: <converted@example.com>

This is an email converted from RFC 5322 (.eml) format to AMF.
`

	// Convert from EML
	converter := msgfmt.NewConverter(nil)
	msg, err := converter.FromEML([]byte(emlData))
	if err != nil {
		log.Fatalf("Failed to convert: %v", err)
	}

	fmt.Printf("✓ Converted from .eml to AMF\n")
	fmt.Printf("✓ Subject: %s\n", msg.Envelope.Subject)
	fmt.Printf("✓ Body: %s\n", msg.Body.Text)

	// Convert back to EML
	emlFile, _ := os.Create("/tmp/converted.eml")
	defer emlFile.Close()

	converter.ToEML(msg, emlFile)
	fmt.Printf("✓ Converted back to .eml\n\n")
}

func example7_ExtendedMessage() {
	fmt.Println("Example 7: Extended Message (with AI metadata)")
	fmt.Println("------------------------------------------------")

	extMsg := msgfmt.NewExtendedMessage(
		"alice@example.com",
		"bob@example.com",
		"Meeting invitation",
	)

	extMsg.SetBody("Hi Bob, let's meet tomorrow at 2 PM to discuss the project.")

	// Add calendar event
	extMsg.CalendarEvent = &msgfmt.CalendarEvent{
		Method:  "REQUEST",
		UID:     "meeting-123",
		Summary: "Project Discussion",
		Start:   time.Now().Add(24 * time.Hour),
		End:     time.Now().Add(25 * time.Hour),
		Location: "Conference Room A",
		Organizer: &msgfmt.Address{
			Address: "alice@example.com",
			Name:    "Alice",
		},
	}

	// Add AI analysis
	extMsg.AI = &msgfmt.AIMetadata{
		Analyzed: true,
		Sentiment: &msgfmt.SentimentAnalysis{
			Overall:    "positive",
			Score:      0.85,
			Confidence: 0.92,
		},
		Classification: &msgfmt.Classification{
			Category:   "business",
			Subcategory: "meeting",
			Confidence: 0.95,
		},
		Entities: []*msgfmt.Entity{
			{
				Type:       "date",
				Text:       "tomorrow at 2 PM",
				Confidence: 0.98,
			},
			{
				Type:       "person",
				Text:       "Bob",
				Confidence: 0.99,
			},
		},
		Intent: &msgfmt.IntentAnalysis{
			Primary:    "request",
			Confidence: 0.94,
		},
	}

	// Save
	writer := msgfmt.NewWriter(&msgfmt.WriterOptions{Indent: true})
	file, _ := os.Create("/tmp/extended_message.amf")
	defer file.Close()

	writer.WriteExtended(file, extMsg)

	fmt.Printf("✓ Extended message created with:\n")
	fmt.Printf("  - Calendar event: %s\n", extMsg.CalendarEvent.Summary)
	fmt.Printf("  - AI sentiment: %s (%.2f)\n", extMsg.AI.Sentiment.Overall, extMsg.AI.Sentiment.Score)
	fmt.Printf("  - Classification: %s\n", extMsg.AI.Classification.Category)
	fmt.Printf("  - Entities: %d detected\n\n", len(extMsg.AI.Entities))
}

func example8_ReplyForward() {
	fmt.Println("Example 8: Reply and Forward")
	fmt.Println("------------------------------")

	// Original message
	original := msgfmt.NewMessage(
		"alice@example.com",
		"bob@example.com",
		"Project update",
	)
	original.SetBody("Hi Bob, here's the latest project update.")

	// Create a reply
	reply := original.BuildReply("bob@example.com")
	reply.SetBody("Thanks Alice! Looks good.")

	fmt.Printf("✓ Reply created\n")
	fmt.Printf("  Subject: %s\n", reply.Envelope.Subject)
	fmt.Printf("  In-Reply-To: %s\n", reply.Envelope.InReplyTo)

	// Create a forward
	forward := original.BuildForward("bob@example.com", "charlie@example.com")
	forward.SetBody("Charlie, FYI - see below.")

	fmt.Printf("✓ Forward created\n")
	fmt.Printf("  Subject: %s\n", forward.Envelope.Subject)
	fmt.Printf("  To: %s\n\n", forward.Envelope.To[0].Address)

	fmt.Println("=== Examples Complete ===")
}
