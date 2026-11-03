package returns

type Client struct{}

type handle struct{}

type state struct{}

type alias = Client

func Open() *handle { return &handle{} } // want `exported func Open returns unexported type \*handle, which can be annoying to use`

func Handles() []handle { return nil } // want `exported func Handles returns unexported type \[\]handle,`

func Pair() [2]handle { return [2]handle{} } // want `exported func Pair returns unexported type \[2\]handle,`

func Feed() chan handle { return nil } // want `exported func Feed returns unexported type chan handle,`

func Index() map[string]handle { return nil } // want `exported func Index returns unexported type map\[string\]handle,`

func Keys() map[handle]bool { return nil } // want `exported func Keys returns unexported type map\[handle\]bool,`

func (Client) State() *state { return &state{} } // want `exported method Client\.State returns unexported type \*state,`

func Split() (int, *handle, *state) { return 0, nil, nil } // want `exported func Split returns unexported type \*handle,`

func Aliased() alias { return Client{} } // want `exported func Aliased returns unexported type alias,`

func Wrap() any { return open() }

func open() *handle { return &handle{} }

func (handle) Close() *handle { return nil }

func New() *Client { return &Client{} }

func Fail() error { return nil }

func Empty() struct{} { return struct{}{} }

func First[T any]() T {
	var zero T
	return zero
}
