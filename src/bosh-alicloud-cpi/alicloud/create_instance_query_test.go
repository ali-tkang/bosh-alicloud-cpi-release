/*
 * Copyright (C) 2017-2019 Alibaba Group Holding Limited
 */
package alicloud

import (
	openapiutil "github.com/alibabacloud-go/openapi-util/service"
	"github.com/alibabacloud-go/tea/tea"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// CreateInstance hands its query map to openapiutil.Query on the way to the
// wire, and the private pool parameters are the first ones this CPI sends whose
// ECS names contain a dot.
//
// Nothing else covers that step. The create_vm tests stop at
// InstanceManagerMock, which records the query map and never reaches the SDK, so
// a Query that dropped or rewrote a dotted key would leave every one of them
// green while the instances quietly kept coming out of the public pool.
var _ = Describe("the query CreateInstance sends", func() {
	stringValues := func(in map[string]*string) map[string]string {
		out := map[string]string{}
		for k, v := range in {
			out[k] = tea.StringValue(v)
		}
		return out
	}

	It("keeps the private pool parameter names intact", func() {
		out := openapiutil.Query(map[string]interface{}{
			"RegionId":                         "cn-hangzhou",
			"PrivatePoolOptions.MatchCriteria": "Target",
			"PrivatePoolOptions.Id":            "eap-bp67acfmxazb4",
		})

		Expect(stringValues(out)).To(Equal(map[string]string{
			"RegionId":                         "cn-hangzhou",
			"PrivatePoolOptions.MatchCriteria": "Target",
			"PrivatePoolOptions.Id":            "eap-bp67acfmxazb4",
		}))
	})

	// Query flattens a nested value by joining the levels with a dot, so naming
	// the parameter in its already-flattened form is not a shortcut past the SDK:
	// it is the same request the nested form produces. If that ever stops being
	// true, this test fails rather than a deployment.
	It("produces the same request as the nested form of the same parameters", func() {
		flat := openapiutil.Query(map[string]interface{}{
			"PrivatePoolOptions.MatchCriteria": "Target",
			"PrivatePoolOptions.Id":            "eap-bp67acfmxazb4",
		})
		nested := openapiutil.Query(map[string]interface{}{
			"PrivatePoolOptions": map[string]interface{}{
				"MatchCriteria": "Target",
				"Id":            "eap-bp67acfmxazb4",
			},
		})

		Expect(stringValues(flat)).To(Equal(stringValues(nested)))
	})
})
