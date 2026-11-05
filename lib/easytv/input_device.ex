defmodule InputDevice do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name))
  end

  @impl true
  def init(opts) do
    receiver = Keyword.fetch!(opts, :receiver)
    path = Keyword.fetch!(opts, :path)
    {:ok, _pid} = InputEvent.start_link(path: path, receiver: receiver)
    {:ok, %{path: path, receiver: receiver}}
  end
end
