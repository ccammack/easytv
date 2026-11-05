defmodule EasyTV.SessionSupervisor do
  use Supervisor

  def start_link(opts), do: Supervisor.start_link(__MODULE__, opts)

  @impl true
  def init(opts) do
    children = [
      {Task.Supervisor, name: App.TaskSupervisor},
      {Osd, []},
      {App.Input,
       [
         name: App.Input,
         channels: opts.channels,
         current_channel: opts.current_channel,
         playback_owner: nil
       ]},
      %{
        id: App.PlaybackOwner,
        start: {__MODULE__, :start_playback_owner, [opts]},
        shutdown: 7000
      },
      {EasyTV.ConfigDispatcher, opts.config},
      %{
        id: RuntimeSupervisor,
        start: {__MODULE__, :start_inputs, [opts.cec_url]},
        type: :supervisor
      }
    ]

    Supervisor.init(children, strategy: :one_for_one, max_restarts: 0)
  end

  def start_playback_owner(opts) do
    with {:ok, pid} <-
           App.PlaybackOwner.start_link(
             name: App.PlaybackOwner,
             input_pid: Process.whereis(App.Input),
             current_channel: opts.current_channel,
             channel_state: opts.channel_state,
             channels: opts.channels
           ) do
      :sys.replace_state(App.Input, &%{&1 | playback_owner: pid})
      {:ok, pid}
    end
  end

  def start_inputs(cec_url) do
    RuntimeSupervisor.start_link(
      receiver: Process.whereis(EasyTV.ConfigDispatcher),
      cec_url: cec_url
    )
  end
end
