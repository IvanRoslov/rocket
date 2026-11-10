export interface ModelProfile {
  name: string;
  enabled: boolean;
  model: string;
}

export function WorkerProfiles({ profiles, picked, onChange, disabled }: {
  profiles: ModelProfile[];
  picked: string[];
  onChange: (names: string[]) => void;
  disabled: boolean;
}) {
  return (
    <fieldset className="creative-worker-profiles" disabled={disabled}>
      <legend>Models allowed for workers</legend>
      {profiles.map((profile) => (
        <label className="check" key={profile.name}>
          <input
            type="checkbox"
            checked={picked.includes(profile.name)}
            onChange={(event) => onChange(event.target.checked
              ? [...picked, profile.name]
              : picked.filter((name) => name !== profile.name))}
          />
          <span>
            {profile.name} · {profile.model}
            {!profile.enabled && " · disabled globally"}
          </span>
        </label>
      ))}
      {profiles.length === 0 && <p>No enabled profiles.</p>}
      <p className="creative-worker-profiles__hint">
        Nothing checked — all profiles. Changes apply to future worker spawns.
      </p>
    </fieldset>
  );
}
