-module(nocontract_json_ffi).
-export([read_json/1]).

read_json(Content) ->
    case string:trim(Content) of
        <<>> -> #{<<"kind">> => <<"empty">>};
        _ -> decode(Content)
    end.

decode(Content) ->
    Decoders = #{
        object_finish => fun(Fields, Parent) ->
            {#{<<"kind">> => <<"object">>, <<"fields">> => lists:reverse(Fields)}, Parent}
        end,
        object_push => fun(Key, Value, Fields) ->
            [{maps:get(<<"text">>, Key), boolean_value(Value)} | Fields]
        end,
        array_finish => fun(Items, Parent) ->
            {#{<<"kind">> => <<"array">>, <<"items">> => lists:reverse(Items)}, Parent}
        end,
        array_push => fun(Value, Items) -> [boolean_value(Value) | Items] end,
        string => fun(Text) -> #{<<"kind">> => <<"text">>, <<"text">> => Text} end,
        integer => fun(Text) -> #{<<"kind">> => <<"number">>, <<"text">> => Text} end,
        float => fun(Text) -> #{<<"kind">> => <<"number">>, <<"text">> => Text} end,
        null => #{<<"kind">> => <<"null">>}
    },
    try json:decode(Content, nil, Decoders) of
        {Value, _, Rest} ->
            case string:trim(Rest) of
                <<>> ->
                    {Restored, []} = restore_numbers(boolean_value(Value), number_tokens(Content)),
                    #{<<"kind">> => <<"read">>, <<"value">> => Restored};
                Remaining -> #{<<"kind">> => <<"multiple">>, <<"remaining">> => Remaining}
            end
    catch
        error:unexpected_end -> malformed(<<"unexpected end of JSON input">>);
        error:{invalid_byte, Byte} -> malformed(format({invalid_byte, Byte}));
        error:{unexpected_sequence, Bytes} -> malformed(format({unexpected_sequence, Bytes}))
    end.

boolean_value(true) -> #{<<"kind">> => <<"boolean">>, <<"value">> => true};
boolean_value(false) -> #{<<"kind">> => <<"boolean">>, <<"value">> => false};
boolean_value(Value) -> Value.

malformed(Reason) -> #{<<"kind">> => <<"malformed">>, <<"reason">> => Reason}.

format(Value) -> unicode:characters_to_binary(io_lib:format("~p", [Value])).

% OTP 29 rewrites number callback lexemes (1e2 -> 1.0e2, -0 -> 0).
% Restoring the source spelling keeps Gleam's number checks identical to Go/Odin.
number_tokens(Content) ->
    Pattern = <<"\"(?:\\\\.|[^\"\\\\])*\"|-?(?:0|[1-9][0-9]*)(?:\\.[0-9]+)?(?:[eE][+-]?[0-9]+)?">>,
    case re:run(Content, Pattern, [global, {capture, first, binary}]) of
        nomatch -> [];
        {match, Tokens} -> [Text || [Text] <- Tokens, binary:first(Text) =/= $"]
    end.

restore_numbers(#{<<"kind">> := <<"number">>} = Value, [Text | Rest]) ->
    {Value#{<<"text">> => Text}, Rest};
restore_numbers(#{<<"kind">> := <<"object">>, <<"fields">> := Fields} = Value, Texts) ->
    {Restored, Remaining} = lists:mapfoldl(
        fun({Key, Child}, Pending) ->
            {Next, Rest} = restore_numbers(Child, Pending),
            {{Key, Next}, Rest}
        end,
        Texts,
        Fields
    ),
    {Value#{<<"fields">> => Restored}, Remaining};
restore_numbers(#{<<"kind">> := <<"array">>, <<"items">> := Items} = Value, Texts) ->
    {Restored, Remaining} = lists:mapfoldl(fun restore_numbers/2, Texts, Items),
    {Value#{<<"items">> => Restored}, Remaining};
restore_numbers(#{<<"kind">> := <<"text">>} = Value, Texts) -> {Value, Texts};
restore_numbers(#{<<"kind">> := <<"boolean">>} = Value, Texts) -> {Value, Texts};
restore_numbers(#{<<"kind">> := <<"null">>} = Value, Texts) -> {Value, Texts}.
