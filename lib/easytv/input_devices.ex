defmodule InputDevices do
  require Logger

  def keyboard_paths do
    by_id_paths =
      "/dev/input/by-id/*-event-kbd"
      |> Path.wildcard()
      |> Enum.filter(&File.exists?/1)
      |> Enum.sort()

    event_paths =
      if by_id_paths == [] do
        "/dev/input/event*"
        |> Path.wildcard()
        |> Enum.filter(&File.regular?/1)
        |> Enum.sort()
      else
        []
      end

    paths = by_id_paths ++ event_paths

    if paths == [] do
      raise "No keyboard input devices found."
    end

    Logger.debug("keyboard_paths=#{inspect(paths)}")
    paths
  end
end
