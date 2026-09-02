/*
 * Copyright (C) 2017-2019 Alibaba Group Holding Limited
 */
package alicloud

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// The exact errors that ended pipeline builds: a DescribeInstances inside the
// attach_disk status wait, and a DescribeDisks in the same wait a build earlier.
const (
	connectionResetFailure = "read tcp 10.80.227.66:40268->47.254.168.135:443: read: connection reset by peer"
	dnsTimeoutFailure      = "dial tcp: lookup ecs.eu-central-1.aliyuncs.com: i/o timeout"
)

var _ = Describe("Invoker", func() {
	// RetryWaitSeconds is 0 so the retry path runs without real sleeps.
	newTestInvoker := func(retryCount int) Invoker {
		i := Invoker{}
		i.AddCatcher(Catcher{"connection reset by peer", retryCount, 0})
		return i
	}

	It("retries a claimed error until the call succeeds", func() {
		invoker := newTestInvoker(6)
		calls := 0

		err := invoker.Run(func() error {
			calls++
			if calls < 3 {
				return errors.New(connectionResetFailure)
			}
			return nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(3))
	})

	It("gives up once the retry budget is spent", func() {
		invoker := newTestInvoker(3)
		calls := 0

		err := invoker.Run(func() error {
			calls++
			return errors.New(connectionResetFailure)
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("over max retry"))
		// A budget of 3 spends one attempt per decrement, so the call is not
		// retried forever when the network stays down.
		Expect(calls).To(Equal(3))
	})

	It("returns an unclaimed error without retrying", func() {
		invoker := newTestInvoker(6)
		calls := 0

		err := invoker.Run(func() error {
			calls++
			return errors.New("InvalidInstanceId.NotFound")
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("InvalidInstanceId.NotFound"))
		Expect(calls).To(Equal(1))
	})

	Describe("NewDescribeInvoker", func() {
		It("claims the transient network failures seen in the pipeline", func() {
			invoker := NewDescribeInvoker()

			By("connection reset by peer")
			Expect(invoker.catcherFor(errors.New(connectionResetFailure))).NotTo(BeNil())
			By("endpoint resolution timing out")
			Expect(invoker.catcherFor(errors.New(dnsTimeoutFailure))).NotTo(BeNil())
		})

		It("still claims what NewInvoker claims", func() {
			invoker := NewDescribeInvoker()

			Expect(invoker.catcherFor(errors.New("ServiceUnavailable"))).NotTo(BeNil())
			Expect(invoker.catcherFor(errors.New("OperationConflict"))).NotTo(BeNil())
			Expect(invoker.catcherFor(errors.New("InternalError"))).NotTo(BeNil())
		})

		It("leaves an API error it should not retry to the caller", func() {
			invoker := NewDescribeInvoker()

			Expect(invoker.catcherFor(errors.New("Forbidden.RAM"))).To(BeNil())
			Expect(invoker.catcherFor(errors.New("OperationDenied.NoStock"))).To(BeNil())
		})
	})

	Describe("NewInvoker", func() {
		// A create or a delete whose response was merely lost must not be
		// replayed, so the plain invoker deliberately does not claim a network
		// failure. Only idempotent describe calls opt into that.
		It("does not retry a network failure", func() {
			invoker := NewInvoker()

			Expect(invoker.catcherFor(errors.New(connectionResetFailure))).To(BeNil())
			Expect(invoker.catcherFor(errors.New(dnsTimeoutFailure))).To(BeNil())
		})
	})

	Describe("the invoker CreateInstance runs under", func() {
		It("claims the create errors that do clear on their own", func() {
			invoker := newCreateInstanceInvoker()

			By("a client token still being processed")
			Expect(invoker.catcherFor(errors.New("LastTokenProcessing"))).NotTo(BeNil())
			By("an idempotent create still being processed")
			Expect(invoker.catcherFor(errors.New("IdempotentProcessing"))).NotTo(BeNil())
			By("an address the previous instance has not released yet")
			Expect(invoker.catcherFor(errors.New("InvalidPrivateIpAddress.Duplicated"))).NotTo(BeNil())
			Expect(invoker.catcherFor(errors.New("InvalidIPAddress.AlreadyUsed"))).NotTo(BeNil())
			By("the service being busy")
			Expect(invoker.catcherFor(errors.New("ServiceUnavailable"))).NotTo(BeNil())
		})

		// A private pool request fails for reasons no amount of waiting fixes, and
		// each must reach the operator as itself. Were any of them claimed here, a
		// create_vm would sit in the retry loop for minutes and then report "over
		// max retry" instead of the reason.
		//
		// The first four codes are the ones ECS actually returns: they were taken
		// from CreateInstance with DryRun set, not from the documentation, so a
		// rename on the ECS side shows up here as a stale test rather than as a
		// retry loop in production. NoStock is the one case DryRun cannot produce,
		// since it needs a pool that is genuinely exhausted.
		It("leaves a private pool rejection to the caller", func() {
			invoker := newCreateInstanceInvoker()

			By("a pool id that does not exist")
			Expect(invoker.catcherFor(errors.New("Invalid.PrivatePoolOptions.Id"))).To(BeNil())
			By("Target asked for with no id")
			Expect(invoker.catcherFor(errors.New("MissingParameter.PrivatePoolOptionsId"))).To(BeNil())
			By("an id supplied with a criteria other than Target")
			Expect(invoker.catcherFor(errors.New("Invalid.PrivatePoolOptionsId"))).To(BeNil())
			By("a private pool asked for on a spot instance")
			Expect(invoker.catcherFor(errors.New("SpotNotSupported"))).To(BeNil())
			By("a Target pool that is out of stock")
			Expect(invoker.catcherFor(errors.New("OperationDenied.NoStock"))).To(BeNil())
		})
	})

	It("gives each invoker its own retry budget", func() {
		// The catchers are package-level values, so a budget spent by one CPI
		// call must not leak into the next. Spending it directly keeps this test
		// off the real retry path, which sleeps between attempts.
		first := NewDescribeInvoker()
		second := NewDescribeInvoker()

		first.catcherFor(errors.New(connectionResetFailure)).RetryCount = 0

		Expect(second.catcherFor(errors.New(connectionResetFailure)).RetryCount).
			To(Equal(ConnectionResetCatcher.RetryCount))
		Expect(ConnectionResetCatcher.RetryCount).To(BeNumerically(">", 0))
	})
})
