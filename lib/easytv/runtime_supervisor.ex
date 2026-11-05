defmodule RuntimeSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    receiver = Keyword.fetch!(opts, :receiver)

    input_children =
      InputDevices.keyboard_paths()
      |> Enum.map(fn path ->
        Supervisor.child_spec({InputDevice, [path: path, receiver: receiver]},
          id: {:input_device, path}
        )
      end)

    cec_children =
      case Keyword.get(opts, :cec_url) do
        nil -> []
        url -> [{EasyTV.Cecmond, url}, {EasyTV.CECStream, url}]
      end

    children = cec_children ++ input_children

    Supervisor.init(children, strategy: :one_for_one)
  end
end
