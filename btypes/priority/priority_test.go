package priority

import (
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/btypes/null"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFloat32AnalogNullAndBinary(t *testing.T) {
	analog := btypes.PropertyData{
		Object: btypes.Object{
			Properties: []btypes.Property{{
				Data: []interface{}{float32(21.5), null.Null{}, float32(3)},
			}},
		},
	}
	got := BuildFloat32(analog, btypes.AnalogOutput)
	require.NotNil(t, got)
	require.NotNil(t, got.P1)
	assert.Equal(t, float32(21.5), *got.P1)
	assert.Nil(t, got.P2)
	require.NotNil(t, got.P3)
	assert.Equal(t, float32(3), *got.P3)

	binary := btypes.PropertyData{
		Object: btypes.Object{
			Properties: []btypes.Property{{
				Data: []interface{}{uint32(1), null.Null{}},
			}},
		},
	}
	got = BuildFloat32(binary, btypes.BinaryOutput)
	require.NotNil(t, got.P1)
	assert.Equal(t, float32(1), *got.P1)
	assert.Nil(t, got.P2)
}
