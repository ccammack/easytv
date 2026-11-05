defmodule EasyTV.Controls do
  require Logger

  @moduledoc """
  Direct, asynchronous application actions.

  `:ok` means the action was sent, not that playback has completed it.
  Command senders return `{:ok, ref}` on dispatch or `{:error, reason}`.
  """

  def channel_next, do: dispatch(:channel_next)
  def channel_previous, do: dispatch(:channel_previous)
  def stop, do: dispatch(:stop)
  def resume, do: dispatch(:resume)
  def send_cec_command(command), do: EasyTV.Cecmond.send_command(command)
  def cec_enabled?, do: Process.whereis(EasyTV.Cecmond) != nil

  def send_mpv_command([name | _] = command) when is_binary(name) do
    with {:ok, _json} <- Jason.encode(command) do
      case Process.whereis(App.PlaybackOwner) do
        nil ->
          Logger.error("mpv command failed: playback owner is not running")
          {:error, :not_running}

        pid ->
          ref = make_ref()
          GenServer.cast(pid, {:mpv_command, ref, command})
          {:ok, ref}
      end
    else
      {:error, reason} ->
        Logger.error("mpv command encoding failed: #{inspect(reason)}")
        {:error, :invalid_command}
    end
  end

  def send_mpv_command(command) do
    Logger.error(
      "mpv command must be a nonempty list starting with a command name: #{inspect(command)}"
    )

    {:error, :invalid_command}
  end

  defp dispatch(action) do
    case Process.whereis(App.Input) do
      nil ->
        Logger.error("control action=#{action} failed: input process is not running")
        {:error, :not_running}

      pid ->
        GenServer.cast(pid, {:control, action})
    end
  end
end
