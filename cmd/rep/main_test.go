package main_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"time"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/bbs/models/test/model_helpers"
	"code.cloudfoundry.org/cfhttp"
	"code.cloudfoundry.org/executor/gardenhealth"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/rep"
	"code.cloudfoundry.org/rep/cmd/rep/testrunner"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/garden/transport"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
)

var runner *testrunner.Runner

var _ = Describe("The Rep", func() {
	var (
		config            testrunner.Config
		fakeGarden        *ghttp.Server
		pollingInterval   time.Duration
		evacuationTimeout time.Duration
		rootFSName        string
		rootFSPath        string
		logger            *lagertest.TestLogger

		flushEvents chan struct{}
	)

	var getActualLRPGroups = func(logger lager.Logger) func() []*models.ActualLRPGroup {
		return func() []*models.ActualLRPGroup {
			actualLRPGroups, err := bbsClient.ActualLRPGroups(logger, models.ActualLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
			return actualLRPGroups
		}
	}

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("test")

		Eventually(getActualLRPGroups(logger), 5*pollingInterval).Should(BeEmpty())
		flushEvents = make(chan struct{})
		fakeGarden = ghttp.NewUnstartedServer()
		// these tests only look for the start of a sequence of requests
		fakeGarden.AllowUnhandledRequests = false
		fakeGarden.RouteToHandler("GET", "/ping", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		fakeGarden.RouteToHandler("GET", "/containers", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		fakeGarden.RouteToHandler("GET", "/capacity", ghttp.RespondWithJSONEncoded(http.StatusOK,
			garden.Capacity{MemoryInBytes: 1024 * 1024 * 1024, DiskInBytes: 2048 * 1024 * 1024, MaxContainers: 4}))
		fakeGarden.RouteToHandler("GET", "/containers/bulk_info", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))

		// The following handlers are needed to fake out the healthcheck containers
		fakeGarden.RouteToHandler("POST", "/containers", ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]string{"handle": "healthcheck-container"}))
		fakeGarden.RouteToHandler("DELETE", regexp.MustCompile("/containers/executor-healthcheck-[-a-f0-9]+"), ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
		fakeGarden.RouteToHandler("POST", "/containers/healthcheck-container/processes", func() http.HandlerFunc {
			firstResponse, err := json.Marshal(transport.ProcessPayload{})
			Expect(err).NotTo(HaveOccurred())

			exitStatus := 0
			secondResponse, err := json.Marshal(transport.ProcessPayload{ExitStatus: &exitStatus})
			Expect(err).NotTo(HaveOccurred())

			headers := http.Header{"Content-Type": []string{"application/json"}}
			response := string(firstResponse) + string(secondResponse)
			return ghttp.RespondWith(http.StatusOK, response, headers)
		}())

		pollingInterval = 50 * time.Millisecond
		evacuationTimeout = 200 * time.Millisecond

		rootFSName = "the-rootfs"
		rootFSPath = "/path/to/rootfs"
		rootFSArg := fmt.Sprintf("%s:%s", rootFSName, rootFSPath)

		config = testrunner.Config{
			PreloadedRootFSes: []string{rootFSArg},
			RootFSProviders:   []string{"docker"},
			CellID:            cellID,
			BBSAddress:        bbsURL.String(),
			ServerPort:        serverPort,
			GardenAddr:        fakeGarden.HTTPTestServer.Listener.Addr().String(),
			LogLevel:          "debug",
			ConsulCluster:     consulRunner.ConsulCluster(),
			PollingInterval:   pollingInterval,
			EvacuationTimeout: evacuationTimeout,
		}

		runner = testrunner.New(
			representativePath,
			config,
		)
	})

	JustBeforeEach(func() {
		runner.Start()
	})

	AfterEach(func(done Done) {
		close(flushEvents)
		runner.KillWithFire()
		fakeGarden.Close()
		close(done)
	})

	Context("when Garden is available", func() {
		BeforeEach(func() {
			fakeGarden.Start()
		})

		Context("when a value is provided caCertsForDownloads", func() {
			var certFile *os.File

			BeforeEach(func() {
				var err error
				certFile, err = ioutil.TempFile("", "")
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				os.Remove(certFile.Name())
			})

			Context("when the file is empty", func() {
				BeforeEach(func() {
					config.CACertsForDownloads = certFile.Name()
					runner = testrunner.New(
						representativePath,
						config,
					)

					runner.StartCheck = "started"
				})

				It("should start", func() {
					Consistently(runner.Session).ShouldNot(Exit())
				})
			})

			Context("when the file has a valid cert bundle", func() {
				BeforeEach(func() {
					fileContents := []byte(`-----BEGIN CERTIFICATE-----
MIIBdzCCASOgAwIBAgIBADALBgkqhkiG9w0BAQUwEjEQMA4GA1UEChMHQWNtZSBD
bzAeFw03MDAxMDEwMDAwMDBaFw00OTEyMzEyMzU5NTlaMBIxEDAOBgNVBAoTB0Fj
bWUgQ28wWjALBgkqhkiG9w0BAQEDSwAwSAJBAN55NcYKZeInyTuhcCwFMhDHCmwa
IUSdtXdcbItRB/yfXGBhiex00IaLXQnSU+QZPRZWYqeTEbFSgihqi1PUDy8CAwEA
AaNoMGYwDgYDVR0PAQH/BAQDAgCkMBMGA1UdJQQMMAoGCCsGAQUFBwMBMA8GA1Ud
EwEB/wQFMAMBAf8wLgYDVR0RBCcwJYILZXhhbXBsZS5jb22HBH8AAAGHEAAAAAAA
AAAAAAAAAAAAAAEwCwYJKoZIhvcNAQEFA0EAAoQn/ytgqpiLcZu9XKbCJsJcvkgk
Se6AbGXgSlq+ZCEVo0qIwSgeBqmsJxUu7NCSOwVJLYNEBO2DtIxoYVk+MA==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIFATCCAuugAwIBAgIBATALBgkqhkiG9w0BAQswEjEQMA4GA1UEAxMHZGllZ29D
QTAeFw0xNjAyMTYyMTU1MzNaFw0yNjAyMTYyMTU1NDZaMBIxEDAOBgNVBAMTB2Rp
ZWdvQ0EwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQC7N7lGx7QGqkMd
wjqgkr09CPoV3HW+GL+YOPajf//CCo15t3mLu9Npv7O7ecb+g/6DxEOtHFpQBSbQ
igzHZkdlBJEGknwH2bsZ4wcVT2vcv2XPAIMDrnT7VuF1S2XD7BJK3n6BeXkFsVPA
OUjC/v0pM/rCFRId5CwtRD/0IHFC/qgEtFQx+zejXXEn1AJMzvNNJ3B0bd8VQGEX
ppemZXS1QvTP7/j2h7fJjosyoL6+76k4mcoScmWFNJHKcG4qcAh8rdnDlw+hJ+5S
z73CadYI2BTnlZ/fxEcsZ/kcteFSf0mFpMYX6vs9/us/rgGwjUNzg+JlzvF43TYY
VQ+TRkFUYHhDv3xwuRHnPNe0Nm30esKpqvbSXtoS6jcnpHn9tMOU0+4NW4aEdy9s
7l4lcGyih4qZfHbYTsRDk1Nrq5EzQbhlZSPC3nxMrLxXri7j22rVCY/Rj9IgAxwC
R3KcCdADGJeNOw44bK/BsRrB+Hxs9yNpXc2V2dez+w3hKNuzyk7WydC3fgXxX6x8
66xnlhFGor7fvM0OSMtGUBD16igh4ySdDiEMNUljqQ1DuMglT1eGdg+Kh+1YYWpz
v3JkNTX96C80IivbZyunZ2CczFhW2HlGWZLwNKeuM0hxt6AmiEa+KJQkx73dfg3L
tkDWWp9TXERPI/6Y2696INi0wElBUQIDAQABo2YwZDAOBgNVHQ8BAf8EBAMCAAYw
EgYDVR0TAQH/BAgwBgEB/wIBADAdBgNVHQ4EFgQU5xGtUKEzsfGmk/Siqo4fgAMs
TBwwHwYDVR0jBBgwFoAU5xGtUKEzsfGmk/Siqo4fgAMsTBwwCwYJKoZIhvcNAQEL
A4ICAQBkWgWl2t5fd4PZ1abpSQNAtsb2lfkkpxcKw+Osn9MeGpcrZjP8XoVTxtUs
GMpeVn2dUYY1sxkVgUZ0Epsgl7eZDK1jn6QfWIjltlHvDtJMh0OrxmdJUuHTGIHc
lsI9NGQRUtbyFHmy6jwIF7q925OmPQ/A6Xgkb45VUJDGNwOMUL5I9LbdBXcjmx6F
ZifEON3wxDBVMIAoS/mZYjP4zy2k1qE2FHoitwDccnCG5Wya+AHdZv/ZlfJcuMtU
U82oyHOctH29BPwASs3E1HUKof6uxJI+Y1M2kBDeuDS7DWiTt3JIVCjewIIhyYYw
uTPbQglqhqHr1RWohliDmKSroIil68s42An0fv9sUr0Btf4itKS1gTb4rNiKTZC/
8sLKs+CA5MB+F8lCllGGFfv1RFiUZBQs9+YEE+ru+yJw39lHeZQsEUgHbLjbVHs1
WFqiKTO8VKl1/eGwG0l9dI26qisIAa/I7kLjlqboKycGDmAAarsmcJBLPzS+ytiu
hoxA/fLhSWJvPXbdGemXLWQGf5DLN/8QGB63Rjp9WC3HhwSoU0NvmNmHoh+AdRRT
dYbCU/DMZjsv+Pt9flhj7ELLo+WKHyI767hJSq9A7IT3GzFt8iGiEAt1qj2yS0DX
36hwbfc1Gh/8nKgFeLmPOlBfKncjTjL2FvBNap6a8tVHXO9FvQ==
-----END CERTIFICATE-----`)

					err := ioutil.WriteFile(certFile.Name(), fileContents, os.ModePerm)
					Expect(err).NotTo(HaveOccurred())

					config.CACertsForDownloads = certFile.Name()
					runner = testrunner.New(
						representativePath,
						config,
					)

					runner.StartCheck = "started"
				})

				It("should start", func() {
					Consistently(runner.Session).ShouldNot(Exit())
				})
			})

			Context("when the file has extra whitespace", func() {
				BeforeEach(func() {
					fileContents := []byte(`
	
						-----BEGIN CERTIFICATE-----
MIIBdzCCASOgAwIBAgIBADALBgkqhkiG9w0BAQUwEjEQMA4GA1UEChMHQWNtZSBD
bzAeFw03MDAxMDEwMDAwMDBaFw00OTEyMzEyMzU5NTlaMBIxEDAOBgNVBAoTB0Fj
bWUgQ28wWjALBgkqhkiG9w0BAQEDSwAwSAJBAN55NcYKZeInyTuhcCwFMhDHCmwa
IUSdtXdcbItRB/yfXGBhiex00IaLXQnSU+QZPRZWYqeTEbFSgihqi1PUDy8CAwEA
AaNoMGYwDgYDVR0PAQH/BAQDAgCkMBMGA1UdJQQMMAoGCCsGAQUFBwMBMA8GA1Ud
EwEB/wQFMAMBAf8wLgYDVR0RBCcwJYILZXhhbXBsZS5jb22HBH8AAAGHEAAAAAAA
AAAAAAAAAAAAAAEwCwYJKoZIhvcNAQEFA0EAAoQn/ytgqpiLcZu9XKbCJsJcvkgk
Se6AbGXgSlq+ZCEVo0qIwSgeBqmsJxUu7NCSOwVJLYNEBO2DtIxoYVk+MA==
-----END CERTIFICATE-----

`)
					err := ioutil.WriteFile(certFile.Name(), fileContents, os.ModePerm)
					Expect(err).NotTo(HaveOccurred())

					config.CACertsForDownloads = certFile.Name()
					runner = testrunner.New(
						representativePath,
						config,
					)
				})

				It("should start", func() {
					Consistently(runner.Session).ShouldNot(Exit())
				})
			})

			Context("when the cert bundle is invalid", func() {
				BeforeEach(func() {
					err := ioutil.WriteFile(certFile.Name(), []byte("invalid cert bundle"), os.ModePerm)
					Expect(err).NotTo(HaveOccurred())

					config.CACertsForDownloads = certFile.Name()
					runner = testrunner.New(
						representativePath,
						config,
					)

					runner.StartCheck = ""
				})

				It("should not start", func() {
					Eventually(runner.Session.Buffer()).Should(gbytes.Say("unable to load CA certificate"))
					Eventually(runner.Session.ExitCode).Should(Equal(1))
				})
			})

			Context("when the file does not exist", func() {
				BeforeEach(func() {
					config.CACertsForDownloads = "does-not-exist"
					runner = testrunner.New(
						representativePath,
						config,
					)

					runner.StartCheck = ""
				})
				It("should not start", func() {
					Eventually(runner.Session.Buffer()).Should(gbytes.Say("failed-to-open-ca-cert-file"))
					Eventually(runner.Session.ExitCode).Should(Equal(1))
				})
			})
		})

		Describe("when an interrupt signal is sent to the representative", func() {
			JustBeforeEach(func() {
				if runtime.GOOS == "windows" {
					Skip("Interrupt isn't supported on windows")
				}

				runner.Stop()
			})

			It("should die", func() {
				Eventually(runner.Session.ExitCode).Should(Equal(0))
			})
		})

		Context("when etcd is down", func() {
			BeforeEach(func() {
				etcdRunner.KillWithFire()
			})

			AfterEach(func() {
				etcdRunner.Start()
			})

			It("starts", func() {
				Consistently(runner.Session).ShouldNot(Exit())
			})
		})

		Context("when starting", func() {
			var deleteChan chan struct{}
			BeforeEach(func() {
				fakeGarden.RouteToHandler("GET", "/containers",
					func(w http.ResponseWriter, r *http.Request) {
						r.ParseForm()
						healthcheckTagQueryParam := gardenhealth.HealthcheckTag
						if r.FormValue(healthcheckTagQueryParam) == gardenhealth.HealthcheckTagValue {
							ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{})(w, r)
						} else {
							ghttp.RespondWithJSONEncoded(http.StatusOK, map[string][]string{"handles": []string{"cnr1", "cnr2"}})(w, r)
						}
					},
				)
				deleteChan = make(chan struct{}, 2)
				fakeGarden.RouteToHandler("DELETE", "/containers/cnr1",
					ghttp.CombineHandlers(
						func(http.ResponseWriter, *http.Request) {
							deleteChan <- struct{}{}
						},
						ghttp.RespondWithJSONEncoded(http.StatusOK, &struct{}{})))
				fakeGarden.RouteToHandler("DELETE", "/containers/cnr2",
					ghttp.CombineHandlers(
						func(http.ResponseWriter, *http.Request) {
							deleteChan <- struct{}{}
						},
						ghttp.RespondWithJSONEncoded(http.StatusOK, &struct{}{})))
			})

			It("destroys any existing containers", func() {
				Eventually(deleteChan).Should(Receive())
				Eventually(deleteChan).Should(Receive())
			})
		})

		Describe("maintaining presence", func() {
			It("should maintain presence", func() {
				Eventually(fetchCells(logger)).Should(HaveLen(1))
				cells, err := bbsClient.Cells(logger)
				cellSet := models.NewCellSetFromList(cells)
				Expect(err).NotTo(HaveOccurred())
				cellPresence := cellSet[cellID]
				Expect(cellPresence.CellId).To(Equal(cellID))
			})
		})

		Context("acting as an auction representative", func() {
			var client rep.Client

			JustBeforeEach(func() {
				Eventually(fetchCells(logger)).Should(HaveLen(1))
				cells, err := bbsClient.Cells(logger)
				cellSet := models.NewCellSetFromList(cells)
				Expect(err).NotTo(HaveOccurred())

				client = rep.NewClient(http.DefaultClient, cfhttp.NewCustomTimeoutClient(100*time.Millisecond), cellSet[cellID].RepAddress)
			})

			Context("Capacity with a container", func() {
				It("returns total capacity", func() {
					state, err := client.State(logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(state.TotalResources).To(Equal(rep.Resources{
						MemoryMB:   1024,
						DiskMB:     2048,
						Containers: 3,
					}))
				})

				Context("when the container is removed", func() {
					It("returns available capacity == total capacity", func() {
						fakeGarden.RouteToHandler("GET", "/containers", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))
						fakeGarden.RouteToHandler("GET", "/containers/bulk_info", ghttp.RespondWithJSONEncoded(http.StatusOK, struct{}{}))

						Eventually(func() rep.Resources {
							state, err := client.State(logger)
							Expect(err).NotTo(HaveOccurred())
							return state.AvailableResources
						}).Should(Equal(rep.Resources{
							MemoryMB:   1024,
							DiskMB:     2048,
							Containers: 3,
						}))
					})
				})
			})
		})

		Describe("polling the BBS for tasks to reap", func() {
			var task *models.Task

			JustBeforeEach(func() {
				task = model_helpers.NewValidTask("task-guid")
				err := bbsClient.DesireTask(logger, task.TaskGuid, task.Domain, task.TaskDefinition)
				Expect(err).NotTo(HaveOccurred())

				_, err = bbsClient.StartTask(logger, task.TaskGuid, cellID)
				Expect(err).NotTo(HaveOccurred())
			})

			It("eventually marks tasks with no corresponding container as failed", func() {
				Eventually(func() []*models.Task {
					return getTasksByState(logger, bbsClient, models.Task_Completed)
				}, 5*pollingInterval).Should(HaveLen(1))

				completedTasks := getTasksByState(logger, bbsClient, models.Task_Completed)

				Expect(completedTasks[0].TaskGuid).To(Equal(task.TaskGuid))
				Expect(completedTasks[0].Failed).To(BeTrue())
			})
		})

		Describe("polling the BBS for actual LRPs to reap", func() {
			JustBeforeEach(func() {
				desiredLRP := &models.DesiredLRP{
					ProcessGuid: "process-guid",
					RootFs:      "some:rootfs",
					Domain:      "some-domain",
					Instances:   1,
					Action: models.WrapAction(&models.RunAction{
						User: "me",
						Path: "the-path",
						Args: []string{},
					}),
				}
				index := 0

				err := bbsClient.DesireLRP(logger, desiredLRP)
				Expect(err).NotTo(HaveOccurred())

				instanceKey := models.NewActualLRPInstanceKey("some-instance-guid", cellID)
				err = bbsClient.ClaimActualLRP(logger, desiredLRP.ProcessGuid, index, &instanceKey)
				Expect(err).NotTo(HaveOccurred())
			})

			It("eventually reaps actual LRPs with no corresponding container", func() {
				Eventually(getActualLRPGroups(logger), 5*pollingInterval).Should(BeEmpty())
			})
		})

		Describe("Evacuation", func() {
			JustBeforeEach(func() {
				resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/evacuate", serverPort), "text/html", nil)
				Expect(err).NotTo(HaveOccurred())
				resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			})

			Context("when exceeding the evacuation timeout", func() {
				It("shuts down gracefully", func() {
					// wait longer than expected to let OS and Go runtime reap process
					Eventually(runner.Session.ExitCode, 2*evacuationTimeout+2*time.Second).Should(Equal(0))
				})
			})

			Context("when signaled to stop", func() {
				JustBeforeEach(func() {
					runner.Stop()
				})

				It("shuts down gracefully", func() {
					Eventually(runner.Session.ExitCode).Should(Equal(0))
				})
			})
		})

		Describe("when a Ping request comes in", func() {
			It("responds with 200 OK", func() {
				resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ping", serverPort))
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
		})
	})

	Context("when Garden is unavailable", func() {
		BeforeEach(func() {
			runner.StartCheck = ""
		})

		It("should not exit and continue waiting for a connection", func() {
			Consistently(runner.Session.Buffer()).ShouldNot(gbytes.Say("started"))
			Consistently(runner.Session).ShouldNot(Exit())
		})

		Context("when Garden starts", func() {
			JustBeforeEach(func() {
				fakeGarden.Start()
				// these tests only look for the start of a sequence of requests
				fakeGarden.AllowUnhandledRequests = false
			})

			It("should connect", func() {
				Eventually(runner.Session.Buffer(), 5*time.Second).Should(gbytes.Say("started"))
			})
		})
	})
})

func getTasksByState(logger lager.Logger, client bbs.InternalClient, state models.Task_State) []*models.Task {
	tasks, err := client.Tasks(logger)
	Expect(err).NotTo(HaveOccurred())

	filteredTasks := make([]*models.Task, 0)
	for _, task := range tasks {
		if task.State == state {
			filteredTasks = append(filteredTasks, task)
		}
	}
	return filteredTasks
}

func fetchCells(logger lager.Logger) func() ([]*models.CellPresence, error) {
	return func() ([]*models.CellPresence, error) {
		return bbsClient.Cells(logger)
	}
}
