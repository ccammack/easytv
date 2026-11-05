import Config

config :logger, level: if(config_env() == :prod, do: :info, else: :debug)

config :logger, :console,
  format: "[$level] $message\n",
  metadata: []
