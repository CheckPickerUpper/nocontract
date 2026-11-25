-module(nocontract_shop_request_deadline_ffi).
-export([read/0]).

read() ->
    case os:getenv("SHOP_TEST_REQUEST_DEADLINE") of
        false ->
            no_test_deadline;
        Milliseconds ->
            {test_deadline, list_to_integer(Milliseconds)}
    end.
