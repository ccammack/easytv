defmodule EasyTV.ConfigDispatcher do
  use GenServer
  require Logger

  def start_link(config) do
    GenServer.start_link(__MODULE__, config, name: __MODULE__)
  end

  def cec_event(event), do: GenServer.cast(__MODULE__, {:cec_event, event})
  def mpv_event(event), do: GenServer.cast(__MODULE__, {:mpv_event, event})

  @impl true
  def init(config), do: {:ok, config}

  @impl true
  def handle_info({:input_event, device, events}, config) when is_list(events) do
    Logger.debug("input device=#{inspect(device)} events=#{inspect(events)}")

    config =
      Enum.reduce(events, config, fn event, config ->
        invoke(:on_key, event, config)
      end)

    {:noreply, config}
  end

  def handle_info(message, config) do
    Logger.warning("unexpected config dispatcher message: #{inspect(message)}")
    {:noreply, config}
  end

  @impl true
  def handle_cast({:mpv_event, event}, config) do
    {:noreply, invoke(:on_mpv, event, config)}
  end

  def handle_cast({:cec_event, event}, config) do
    {:noreply, invoke(:on_cec, EasyTV.CECObservations.interpret(event), config)}
  end

  @impl true
  def handle_call({:cec_event, event}, _from, config) do
    {:reply, :ok, invoke(:on_cec, EasyTV.CECObservations.interpret(event), config)}
  end

  defp invoke(callback, event, config) do
    state = Map.fetch!(config, callback).(event, config.state)
    %{config | state: state}
  rescue
    error ->
      Logger.error(
        "config #{callback} failed event=#{inspect(event)} state=#{inspect(config.state)}\n" <>
          Exception.format(:error, error, __STACKTRACE__)
      )

      reraise error, __STACKTRACE__
  end
end
