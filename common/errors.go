package common

import "fmt"

var (
	ErrInvalidValues  = fmt.Errorf("invalid values")
	ErrNoRowsAffected = fmt.Errorf("no rows affected")

	// ErrPrePayment wraps errors that occur before any payment is attempted
	// (DB lookups, missing card details). No money moved. No point retrying
	// on another terminal since the same issue will occur.
	ErrPrePayment = fmt.Errorf("pre-payment error")

	// ErrPostPayment wraps errors that occur after the payment gateway call
	// (DB updates after successful/failed payment). Money may have moved.
	// MUST NOT retry on another terminal to avoid double-charging.
	ErrPostPayment = fmt.Errorf("post-payment error")

	// Coupon redemption outcomes. ErrCouponInvalid is the uniform response for
	// unknown/disabled/out-of-window codes (details stay in logs); the rest are
	// specific because a legitimate member's clarity outweighs the disclosure.
	ErrCouponInvalid         = fmt.Errorf("invalid or expired code")
	ErrCouponAlreadyRedeemed = fmt.Errorf("coupon already redeemed")
	ErrCouponExhausted       = fmt.Errorf("coupon no longer available")
	ErrCouponCountryMismatch = fmt.Errorf("coupon not available in your country")
	ErrCouponCountryRequired = fmt.Errorf("set your country first")
	ErrCouponCodeConflict    = fmt.Errorf("coupon code already exists")
)
