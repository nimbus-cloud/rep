package rep_test

import (
	"net/http"
	"time"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/cfhttp"
	"code.cloudfoundry.org/rep"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var _ = Describe("Client", func() {
	var fakeServer *ghttp.Server
	var client rep.Client

	BeforeEach(func() {
		fakeServer = ghttp.NewServer()
		client = rep.NewClient(cfhttp.NewClient(), nil, fakeServer.URL())
	})

	AfterEach(func() {
		fakeServer.Close()
	})

	Describe("StopLRPInstance", func() {
		const cellAddr = "cell.example.com"
		var stopErr error
		var actualLRP = models.ActualLRP{
			ActualLRPKey:         models.NewActualLRPKey("some-process-guid", 2, "test-domain"),
			ActualLRPInstanceKey: models.NewActualLRPInstanceKey("some-instance-guid", "some-cell-id"),
		}

		JustBeforeEach(func() {
			stopErr = client.StopLRPInstance(actualLRP.ActualLRPKey, actualLRP.ActualLRPInstanceKey)
		})

		Context("when the request is successful", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/lrps/some-process-guid/instances/some-instance-guid/stop"),
						ghttp.RespondWith(http.StatusAccepted, ""),
					),
				)
			})

			It("makes the request and does not return an error", func() {
				Expect(stopErr).NotTo(HaveOccurred())
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})

		Context("when the request returns 500", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/lrps/some-process-guid/instances/some-instance-guid/stop"),
						ghttp.RespondWith(http.StatusInternalServerError, ""),
					),
				)
			})

			It("makes the request and returns an error", func() {
				Expect(stopErr).To(HaveOccurred())
				Expect(stopErr.Error()).To(ContainSubstring("http error: status code 500"))
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})

		Context("when the connection fails", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/lrps/some-process-guid/instances/some-instance-guid/stop"),
						func(w http.ResponseWriter, r *http.Request) {
							fakeServer.CloseClientConnections()
						},
					),
				)
			})

			It("makes the request and returns an error", func() {
				Expect(stopErr).To(HaveOccurred())
				Expect(stopErr.Error()).To(ContainSubstring("EOF"))
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})

		Context("when the connection times out", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/lrps/some-process-guid/instances/some-instance-guid/stop"),
						func(w http.ResponseWriter, r *http.Request) {
							time.Sleep(cfHttpTimeout + 100*time.Millisecond)
						},
					),
				)
			})

			It("makes the request and returns an error", func() {
				Expect(stopErr).To(HaveOccurred())
				Expect(stopErr.Error()).To(MatchRegexp("use of closed network connection|Client.Timeout exceeded while awaiting headers"))
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})
	})

	Describe("CancelTask", func() {
		const cellAddr = "cell.example.com"
		var cancelErr error
		var taskGuid = "some-task-guid"

		JustBeforeEach(func() {
			cancelErr = client.CancelTask(taskGuid)
		})

		Context("when the request is successful", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/tasks/some-task-guid/cancel"),
						ghttp.RespondWith(http.StatusAccepted, ""),
					),
				)
			})

			It("makes the request and does not return an error", func() {
				Expect(cancelErr).NotTo(HaveOccurred())
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})

		Context("when the request returns 500", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/tasks/some-task-guid/cancel"),
						ghttp.RespondWith(http.StatusInternalServerError, ""),
					),
				)
			})

			It("makes the request and returns an error", func() {
				Expect(cancelErr).To(HaveOccurred())
				Expect(cancelErr.Error()).To(ContainSubstring("http error: status code 500"))
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})

		Context("when the connection fails", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/tasks/some-task-guid/cancel"),
						func(w http.ResponseWriter, r *http.Request) {
							fakeServer.CloseClientConnections()
						},
					),
				)
			})

			It("makes the request and returns an error", func() {
				Expect(cancelErr).To(HaveOccurred())
				Expect(cancelErr.Error()).To(ContainSubstring("EOF"))
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})

		Context("when the connection times out", func() {
			BeforeEach(func() {
				fakeServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/v1/tasks/some-task-guid/cancel"),
						func(w http.ResponseWriter, r *http.Request) {
							time.Sleep(cfHttpTimeout + 100*time.Millisecond)
						},
					),
				)
			})

			It("makes the request and returns an error", func() {
				Expect(cancelErr).To(HaveOccurred())
				Expect(cancelErr.Error()).To(MatchRegexp("use of closed network connection|Client.Timeout exceeded while awaiting headers"))
				Expect(fakeServer.ReceivedRequests()).To(HaveLen(1))
			})
		})
	})
})
