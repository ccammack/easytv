defmodule EasyTV.Application do
  use Application

  @impl true
  def start(_type, _args) do
    :persistent_term.put({__MODULE__, :stopping}, false)

    DynamicSupervisor.start_link(
      strategy: :one_for_one,
      max_restarts: 0,
      name: EasyTV.Supervisor
    )
  end

  @impl true
  def prep_stop(state) do
    :persistent_term.put({__MODULE__, :stopping}, true)
    state
  end

  def stopping?, do: :persistent_term.get({__MODULE__, :stopping}, false)
end
