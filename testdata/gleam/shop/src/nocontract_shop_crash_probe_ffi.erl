-module(nocontract_shop_crash_probe_ffi).
-export([bounds/2]).

bounds(Values, Index) -> element(Index, Values).
