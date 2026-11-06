defmodule EasyTV.CurrentMediaTest do
  use ExUnit.Case
  import ExUnit.CaptureLog

  defmodule QueryOwner do
    use GenServer

    def start_link(state), do: GenServer.start_link(__MODULE__, state, name: App.PlaybackOwner)
    def init(state), do: {:ok, state}

    def handle_call(request, from, state),
      do: App.PlaybackOwner.handle_call(request, from, state)
  end

  test "query returns original paths and media kinds without modifying playback state" do
    start_supervised!({QueryOwner, %App.PlaybackOwner.State{}})

    for {path, kind} <- [
          {"/media/photos/Photo.JPG", :image},
          {"/media/music/Song.flac", :audio},
          {"/media/movies/Film.mp4", :video}
        ] do
      state = %App.PlaybackOwner.State{
        current_player: self(),
        current_channel: %{media: [path]},
        current_file_index: 0
      }

      :sys.replace_state(App.PlaybackOwner, fn _ -> state end)

      assert {:ok, %{path: ^path, kind: ^kind, playback_id: id}} =
               EasyTV.Controls.current_media()

      assert id == self()
      assert :sys.get_state(App.PlaybackOwner) == state
    end
  end

  test "replacing a player changes identity even for the same original file" do
    player =
      spawn(fn ->
        receive do
          :stop -> :ok
        end
      end)

    on_exit(fn -> send(player, :stop) end)

    state = %App.PlaybackOwner.State{
      current_player: self(),
      current_channel: %{media: ["/media/photo.png"]},
      current_file_index: 0
    }

    start_supervised!({QueryOwner, state})
    assert {:ok, first} = EasyTV.Controls.current_media()
    :sys.replace_state(App.PlaybackOwner, &%{&1 | current_player: player})
    assert {:ok, second} = EasyTV.Controls.current_media()
    assert first.path == second.path
    assert first.playback_id != second.playback_id
  end

  test "stopped playback and an absent owner report failures" do
    assert capture_log(fn ->
             assert {:error, :not_running} = EasyTV.Controls.current_media()
             start_supervised!({QueryOwner, %App.PlaybackOwner.State{}})
             assert {:error, :no_player} = EasyTV.Controls.current_media()
           end) =~ "current media query failed"
  end
end
