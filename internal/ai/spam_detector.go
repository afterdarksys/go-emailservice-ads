package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SpamDetector implements AI-powered spam detection
// Uses a combination of Naive Bayes classification and heuristic analysis
type SpamDetector struct {
	logger *zap.Logger

	// Naive Bayes model
	spamWordCount map[string]int
	hamWordCount  map[string]int
	spamTotal     int
	hamTotal      int

	// Training data
	trainLock sync.RWMutex

	// Feature weights learned from training
	featureWeights map[string]float64

	// Heuristic thresholds
	spamThreshold float64
}

// SpamScore represents spam classification result
type SpamScore struct {
	Score       float64           // 0.0 (ham) to 1.0 (spam)
	IsSpam      bool              // true if score > threshold
	Confidence  float64           // 0.0 to 1.0
	Reasons     []string          // Human-readable reasons
	Features    map[string]float64 // Feature contributions
}

// NewSpamDetector creates a new spam detector with pre-trained data
func NewSpamDetector(logger *zap.Logger) *SpamDetector {
	sd := &SpamDetector{
		logger:         logger,
		spamWordCount:  make(map[string]int),
		hamWordCount:   make(map[string]int),
		featureWeights: make(map[string]float64),
		spamThreshold:  0.7, // 70% confidence threshold
	}

	// Initialize with common spam patterns
	sd.initializeSpamPatterns()

	return sd
}

// AnalyzeMessage analyzes a message and returns a spam score
func (s *SpamDetector) AnalyzeMessage(from, subject, body string) *SpamScore {
	s.trainLock.RLock()
	defer s.trainLock.RUnlock()

	features := s.extractFeatures(from, subject, body)
	score := s.calculateBayesianScore(features)

	// Apply heuristic adjustments
	heuristicBoost := s.applyHeuristics(from, subject, body)
	finalScore := math.Min(1.0, score+heuristicBoost)

	reasons := s.generateReasons(features, heuristicBoost)
	confidence := s.calculateConfidence(finalScore, features)

	result := &SpamScore{
		Score:      finalScore,
		IsSpam:     finalScore >= s.spamThreshold,
		Confidence: confidence,
		Reasons:    reasons,
		Features:   features,
	}

	s.logger.Debug("Spam analysis complete",
		zap.String("from", from),
		zap.Float64("score", finalScore),
		zap.Bool("is_spam", result.IsSpam),
		zap.Float64("confidence", confidence))

	return result
}

// extractFeatures extracts classification features from message
func (s *SpamDetector) extractFeatures(from, subject, body string) map[string]float64 {
	features := make(map[string]float64)

	// Tokenize and extract word frequencies
	words := s.tokenize(subject + " " + body)
	for _, word := range words {
		features["word:"+word] += 1.0
	}

	// Subject-specific features
	features["subject_length"] = float64(len(subject))
	features["subject_uppercase_ratio"] = s.uppercaseRatio(subject)
	features["subject_exclamation_count"] = float64(strings.Count(subject, "!"))
	features["subject_question_count"] = float64(strings.Count(subject, "?"))

	// Body features
	features["body_length"] = float64(len(body))
	features["body_uppercase_ratio"] = s.uppercaseRatio(body)
	features["body_url_count"] = float64(len(s.extractURLs(body)))
	features["body_html_tag_count"] = float64(s.countHTMLTags(body))

	// From address features
	features["from_length"] = float64(len(from))
	features["from_has_numbers"] = s.boolToFloat(s.hasNumbers(from))
	features["from_suspicious_tld"] = s.boolToFloat(s.hasSuspiciousTLD(from))

	// Spammy patterns
	features["contains_unsubscribe"] = s.boolToFloat(s.containsPattern(body, `unsubscribe|opt-out`))
	features["contains_viagra"] = s.boolToFloat(s.containsPattern(body, `viagra|cialis|pharmacy`))
	features["contains_casino"] = s.boolToFloat(s.containsPattern(body, `casino|poker|lottery`))
	features["contains_urgent"] = s.boolToFloat(s.containsPattern(subject+body, `urgent|act now|limited time`))
	features["contains_money"] = s.boolToFloat(s.containsPattern(body, `\$\$\$|make money|earn cash`))

	return features
}

// calculateBayesianScore calculates spam probability using Naive Bayes
func (s *SpamDetector) calculateBayesianScore(features map[string]float64) float64 {
	if s.spamTotal == 0 || s.hamTotal == 0 {
		// No training data - use heuristics only
		return 0.5
	}

	logProbSpam := math.Log(float64(s.spamTotal) / float64(s.spamTotal+s.hamTotal))
	logProbHam := math.Log(float64(s.hamTotal) / float64(s.spamTotal+s.hamTotal))

	// Calculate log probabilities for each word feature
	for feature, count := range features {
		if strings.HasPrefix(feature, "word:") {
			word := feature[5:]

			// Laplace smoothing
			spamProb := float64(s.spamWordCount[word]+1) / float64(s.spamTotal+len(s.spamWordCount))
			hamProb := float64(s.hamWordCount[word]+1) / float64(s.hamTotal+len(s.hamWordCount))

			logProbSpam += count * math.Log(spamProb)
			logProbHam += count * math.Log(hamProb)
		}
	}

	// Convert log probabilities to probability
	probSpam := math.Exp(logProbSpam)
	probHam := math.Exp(logProbHam)

	return probSpam / (probSpam + probHam)
}

// applyHeuristics applies rule-based spam detection
func (s *SpamDetector) applyHeuristics(from, subject, body string) float64 {
	boost := 0.0

	// ALL CAPS subject
	if len(subject) > 0 && s.uppercaseRatio(subject) > 0.7 {
		boost += 0.15
	}

	// Excessive punctuation
	if strings.Count(subject, "!") >= 3 {
		boost += 0.1
	}

	// Suspicious sender patterns
	if s.hasNumbers(from) && s.hasSuspiciousTLD(from) {
		boost += 0.2
	}

	// Too many URLs
	urlCount := len(s.extractURLs(body))
	if urlCount > 5 {
		boost += 0.1
	}
	if urlCount > 10 {
		boost += 0.2
	}

	// Known spam keywords in subject
	spamKeywords := []string{"URGENT", "ACT NOW", "LIMITED TIME", "FREE MONEY", "CLICK HERE"}
	for _, keyword := range spamKeywords {
		if strings.Contains(strings.ToUpper(subject), keyword) {
			boost += 0.1
			break
		}
	}

	return boost
}

// generateReasons provides human-readable explanation
func (s *SpamDetector) generateReasons(features map[string]float64, heuristicBoost float64) []string {
	reasons := make([]string, 0)

	if features["subject_uppercase_ratio"] > 0.7 {
		reasons = append(reasons, "Subject is mostly uppercase")
	}

	if features["subject_exclamation_count"] >= 3 {
		reasons = append(reasons, "Excessive exclamation marks in subject")
	}

	if features["body_url_count"] > 5 {
		reasons = append(reasons, fmt.Sprintf("High URL count (%d)", int(features["body_url_count"])))
	}

	if features["from_suspicious_tld"] > 0 {
		reasons = append(reasons, "Suspicious sender domain")
	}

	if features["contains_viagra"] > 0 || features["contains_casino"] > 0 {
		reasons = append(reasons, "Contains pharmaceutical or gambling keywords")
	}

	if features["contains_urgent"] > 0 {
		reasons = append(reasons, "Contains urgency manipulation")
	}

	if heuristicBoost > 0.2 {
		reasons = append(reasons, "Multiple spam indicators detected")
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "Bayesian analysis indicates spam probability")
	}

	return reasons
}

// calculateConfidence returns confidence level for the classification
func (s *SpamDetector) calculateConfidence(score float64, features map[string]float64) float64 {
	// Confidence based on distance from threshold
	distance := math.Abs(score - 0.5)
	confidence := distance * 2.0 // Scale to 0-1

	// Boost confidence if we have strong feature signals
	strongFeatures := 0
	for _, value := range features {
		if value > 0 {
			strongFeatures++
		}
	}

	if strongFeatures > 5 {
		confidence = math.Min(1.0, confidence+0.1)
	}

	return confidence
}

// Train trains the spam detector with labeled data
func (s *SpamDetector) Train(from, subject, body string, isSpam bool) {
	s.trainLock.Lock()
	defer s.trainLock.Unlock()

	words := s.tokenize(subject + " " + body)

	if isSpam {
		s.spamTotal++
		for _, word := range words {
			s.spamWordCount[word]++
		}
	} else {
		s.hamTotal++
		for _, word := range words {
			s.hamWordCount[word]++
		}
	}

	s.logger.Debug("Spam detector trained",
		zap.Bool("is_spam", isSpam),
		zap.Int("word_count", len(words)))
}

// Helper methods

func (s *SpamDetector) tokenize(text string) []string {
	// Convert to lowercase and split on non-alphanumeric
	text = strings.ToLower(text)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	parts := re.Split(text, -1)

	// Filter out short and stop words
	words := make([]string, 0)
	stopWords := map[string]bool{
		"the": true, "is": true, "at": true, "which": true, "on": true,
		"a": true, "an": true, "and": true, "or": true, "but": true,
	}

	for _, word := range parts {
		if len(word) >= 3 && !stopWords[word] {
			words = append(words, word)
		}
	}

	return words
}

func (s *SpamDetector) uppercaseRatio(text string) float64 {
	if len(text) == 0 {
		return 0.0
	}

	uppercase := 0
	for _, r := range text {
		if r >= 'A' && r <= 'Z' {
			uppercase++
		}
	}

	return float64(uppercase) / float64(len(text))
}

func (s *SpamDetector) extractURLs(text string) []string {
	re := regexp.MustCompile(`https?://[^\s]+`)
	return re.FindAllString(text, -1)
}

func (s *SpamDetector) countHTMLTags(text string) int {
	re := regexp.MustCompile(`<[^>]+>`)
	return len(re.FindAllString(text, -1))
}

func (s *SpamDetector) hasNumbers(text string) bool {
	re := regexp.MustCompile(`\d`)
	return re.MatchString(text)
}

func (s *SpamDetector) hasSuspiciousTLD(email string) bool {
	suspiciousTLDs := []string{".xyz", ".top", ".click", ".link", ".online", ".site"}
	emailLower := strings.ToLower(email)
	for _, tld := range suspiciousTLDs {
		if strings.HasSuffix(emailLower, tld) {
			return true
		}
	}
	return false
}

func (s *SpamDetector) containsPattern(text, pattern string) bool {
	re := regexp.MustCompile(`(?i)` + pattern)
	return re.MatchString(text)
}

func (s *SpamDetector) boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// initializeSpamPatterns initializes with common spam patterns
func (s *SpamDetector) initializeSpamPatterns() {
	// Pre-populate with known spam words
	spamWords := []string{
		"viagra", "cialis", "pharmacy", "casino", "poker", "lottery",
		"prize", "winner", "congratulations", "claim", "urgent", "act",
		"limited", "offer", "discount", "free", "bonus", "cash", "money",
	}

	for _, word := range spamWords {
		s.spamWordCount[word] = 10 // Give high initial weight
		s.spamTotal += 10
	}

	// Pre-populate with known ham words
	hamWords := []string{
		"meeting", "schedule", "report", "update", "project", "team",
		"document", "review", "please", "thanks", "regards", "invoice",
	}

	for _, word := range hamWords {
		s.hamWordCount[word] = 10
		s.hamTotal += 10
	}
}

// GetStats returns spam detector statistics
func (s *SpamDetector) GetStats() map[string]interface{} {
	s.trainLock.RLock()
	defer s.trainLock.RUnlock()

	return map[string]interface{}{
		"spam_samples":  s.spamTotal,
		"ham_samples":   s.hamTotal,
		"vocabulary_size": len(s.spamWordCount) + len(s.hamWordCount),
		"threshold":     s.spamThreshold,
	}
}

// BouncePredictor predicts if an email will bounce before sending
type BouncePredictor struct {
	logger *zap.Logger

	// Historical bounce data
	bounceHistory map[string]*BounceStats
	mu            sync.RWMutex
}

// BounceStats tracks bounce statistics for a domain/address
type BounceStats struct {
	Domain       string
	TotalSent    int
	TotalBounced int
	LastBounce   time.Time
	BounceReasons map[string]int
}

// BouncePrediction represents bounce prediction result
type BouncePrediction struct {
	WillBounce  bool
	Probability float64
	Confidence  float64
	Reasons     []string
}

// NewBouncePredictor creates a new bounce predictor
func NewBouncePredictor(logger *zap.Logger) *BouncePredictor {
	return &BouncePredictor{
		logger:        logger,
		bounceHistory: make(map[string]*BounceStats),
	}
}

// Predict predicts if an email will bounce
func (b *BouncePredictor) Predict(recipientEmail, recipientDomain string) *BouncePrediction {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Check domain history
	domainStats, hasDomainHistory := b.bounceHistory[recipientDomain]
	emailStats, hasEmailHistory := b.bounceHistory[recipientEmail]

	probability := 0.0
	reasons := make([]string, 0)

	// Calculate probability based on history
	if hasEmailHistory && emailStats.TotalSent > 0 {
		emailBounceRate := float64(emailStats.TotalBounced) / float64(emailStats.TotalSent)
		probability = emailBounceRate

		if emailBounceRate > 0.5 {
			reasons = append(reasons, fmt.Sprintf("Address has %d%% bounce rate", int(emailBounceRate*100)))
		}

		// Recent bounce increases probability
		if time.Since(emailStats.LastBounce) < 24*time.Hour {
			probability += 0.2
			reasons = append(reasons, "Recent bounce detected")
		}
	} else if hasDomainHistory && domainStats.TotalSent > 0 {
		domainBounceRate := float64(domainStats.TotalBounced) / float64(domainStats.TotalSent)
		probability = domainBounceRate * 0.7 // Lower weight for domain-level

		if domainBounceRate > 0.3 {
			reasons = append(reasons, fmt.Sprintf("Domain has %d%% bounce rate", int(domainBounceRate*100)))
		}
	}

	// Heuristic checks
	if !b.isValidEmailFormat(recipientEmail) {
		probability += 0.5
		reasons = append(reasons, "Invalid email format")
	}

	if b.isDisposableEmail(recipientDomain) {
		probability += 0.3
		reasons = append(reasons, "Disposable email domain")
	}

	probability = math.Min(1.0, probability)
	confidence := b.calculateConfidence(hasEmailHistory, hasDomainHistory)

	return &BouncePrediction{
		WillBounce:  probability > 0.6,
		Probability: probability,
		Confidence:  confidence,
		Reasons:     reasons,
	}
}

// RecordBounce records a bounce for learning
func (b *BouncePredictor) RecordBounce(recipientEmail, recipientDomain, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Record for email
	if _, exists := b.bounceHistory[recipientEmail]; !exists {
		b.bounceHistory[recipientEmail] = &BounceStats{
			Domain:        recipientDomain,
			BounceReasons: make(map[string]int),
		}
	}
	stats := b.bounceHistory[recipientEmail]
	stats.TotalBounced++
	stats.LastBounce = time.Now()
	stats.BounceReasons[reason]++

	// Record for domain
	if _, exists := b.bounceHistory[recipientDomain]; !exists {
		b.bounceHistory[recipientDomain] = &BounceStats{
			Domain:        recipientDomain,
			BounceReasons: make(map[string]int),
		}
	}
	domainStats := b.bounceHistory[recipientDomain]
	domainStats.TotalBounced++
	domainStats.LastBounce = time.Now()
	domainStats.BounceReasons[reason]++

	b.logger.Info("Bounce recorded",
		zap.String("email", recipientEmail),
		zap.String("reason", reason))
}

// RecordSuccess records a successful delivery
func (b *BouncePredictor) RecordSuccess(recipientEmail, recipientDomain string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.bounceHistory[recipientEmail]; !exists {
		b.bounceHistory[recipientEmail] = &BounceStats{
			Domain:        recipientDomain,
			BounceReasons: make(map[string]int),
		}
	}
	b.bounceHistory[recipientEmail].TotalSent++

	if _, exists := b.bounceHistory[recipientDomain]; !exists {
		b.bounceHistory[recipientDomain] = &BounceStats{
			Domain:        recipientDomain,
			BounceReasons: make(map[string]int),
		}
	}
	b.bounceHistory[recipientDomain].TotalSent++
}

func (b *BouncePredictor) isValidEmailFormat(email string) bool {
	re := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return re.MatchString(email)
}

func (b *BouncePredictor) isDisposableEmail(domain string) bool {
	disposableDomains := []string{
		"tempmail.com", "10minutemail.com", "guerrillamail.com",
		"mailinator.com", "throwaway.email",
	}

	domainLower := strings.ToLower(domain)
	for _, disposable := range disposableDomains {
		if domainLower == disposable {
			return true
		}
	}
	return false
}

func (b *BouncePredictor) calculateConfidence(hasEmailHistory, hasDomainHistory bool) float64 {
	if hasEmailHistory {
		return 0.9 // High confidence with email-level data
	} else if hasDomainHistory {
		return 0.6 // Medium confidence with domain-level data
	}
	return 0.3 // Low confidence without history
}
