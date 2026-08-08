/**
 * What a manager writes about a building and about a flat (#354).
 *
 * A renter decides on the description and the amenities long before anybody
 * shows them the rent, so both are held on the record and read back from it.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';

export type PropertyDescription = {
  loading: boolean;
  error?: string;
  name?: string;
  about: string;
  amenities: string[];
  setAbout: (s: string) => void;
  toggle: (amenity: string) => void;
  save: () => Promise<void>;
};

export type UnitDescription = {
  loading: boolean;
  error?: string;
  name?: string;
  about: string;
  features: string[];
  bathrooms: string;
  balconies: string;
  coveredParking: string;
  facing: string;
  furnishing: string;
  setAbout: (s: string) => void;
  setBathrooms: (s: string) => void;
  setBalconies: (s: string) => void;
  setCoveredParking: (s: string) => void;
  setFacing: (s: string) => void;
  setFurnishing: (s: string) => void;
  toggle: (feature: string) => void;
  save: () => Promise<void>;
};

/** The vocabularies are CHECK constraints on the record; the app offers the
 *  same words so a manager cannot file one nobody can search for. */
export const amenityVocabulary = [
  'lift', 'power_backup', 'security', 'cctv', 'gated', 'visitor_parking',
  'water_supply_24x7', 'borewell', 'rainwater_harvesting', 'sewage_treatment',
  'gym', 'swimming_pool', 'clubhouse', 'play_area', 'park', 'jogging_track',
  'community_hall', 'temple', 'shops_in_complex', 'waste_collection',
  'fire_safety', 'intercom', 'solar', 'ev_charging',
];

export const featureVocabulary = [
  'modular_kitchen', 'wardrobes', 'false_ceiling', 'chimney', 'geyser',
  'air_conditioning', 'piped_gas', 'water_purifier', 'washing_machine_point',
  'dishwasher_point', 'study', 'servant_room', 'pooja_room', 'store_room',
  'utility_area', 'private_terrace', 'garden_access', 'attached_bathroom',
  'western_toilet', 'wooden_flooring', 'vitrified_flooring', 'marble_flooring',
  'power_backup_point', 'internet_ready', 'pet_friendly',
];

export const facings = ['north', 'south', 'east', 'west',
  'north_east', 'north_west', 'south_east', 'south_west'];

export const furnishings = ['unfurnished', 'semi_furnished', 'fully_furnished'];

function switched(held: string[], value: string): string[] {
  return held.includes(value) ? held.filter((v) => v !== value) : [...held, value];
}

// A blank count is "not recorded", which is not the same as none — the record
// holds nothing rather than a zero somebody would read as no bathroom.
function counted(label: string, raw: string): number | undefined {
  const s = raw.trim();
  if (!s) return undefined;
  const n = Number(s);
  if (!Number.isInteger(n) || n < 0) throw new Error(`${label} has to be a number.`);
  return n;
}

export function usePropertyDescription(id: string | undefined): PropertyDescription {
  const api = useMemo(() => apiFromEnv(), []);
  const [loading, setLoading] = useState(Boolean(api && id));
  const [error, setError] = useState<string | undefined>();
  const [name, setName] = useState<string | undefined>();
  const [about, setAbout] = useState('');
  const [amenities, setAmenities] = useState<string[]>([]);

  useEffect(() => {
    if (!api || !id) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    api.opsProperty(id)
      .then(({ property }) => {
        if (!alive) return;
        setName(property.name);
        setAbout(property.about ?? '');
        setAmenities(property.amenities ?? []);
        setError(undefined);
      })
      .catch((err: Error) => { if (alive) setError(err.message); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [api, id]);

  const save = useCallback(async () => {
    if (!api || !id) throw new Error('The API is not configured on this build.');
    await api.opsDescribeProperty(id, about.trim(), amenities);
  }, [api, id, about, amenities]);

  return {
    loading, error, name, about, amenities,
    setAbout,
    toggle: useCallback((a: string) => setAmenities((held) => switched(held, a)), []),
    save,
  };
}

export function useUnitDescription(id: string | undefined): UnitDescription {
  const api = useMemo(() => apiFromEnv(), []);
  const [loading, setLoading] = useState(Boolean(api && id));
  const [error, setError] = useState<string | undefined>();
  const [name, setName] = useState<string | undefined>();
  const [about, setAbout] = useState('');
  const [features, setFeatures] = useState<string[]>([]);
  const [bathrooms, setBathrooms] = useState('');
  const [balconies, setBalconies] = useState('');
  const [coveredParking, setCoveredParking] = useState('');
  const [facing, setFacing] = useState('');
  const [furnishing, setFurnishing] = useState('');

  useEffect(() => {
    if (!api || !id) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    api.opsUnit(id)
      .then(({ unit }) => {
        if (!alive) return;
        setName(unit.code);
        setAbout(unit.about ?? '');
        setFeatures(unit.features ?? []);
        setBathrooms(unit.bathrooms === undefined ? '' : String(unit.bathrooms));
        setBalconies(unit.balconies === undefined ? '' : String(unit.balconies));
        setCoveredParking(unit.covered_parking === undefined ? '' : String(unit.covered_parking));
        setFacing(unit.facing ?? '');
        setFurnishing(unit.furnishing ?? '');
        setError(undefined);
      })
      .catch((err: Error) => { if (alive) setError(err.message); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [api, id]);

  const save = useCallback(async () => {
    if (!api || !id) throw new Error('The API is not configured on this build.');
    await api.opsDescribeUnit(id, {
      about: about.trim(),
      features,
      bathrooms: counted('Bathrooms', bathrooms),
      balconies: counted('Balconies', balconies),
      covered_parking: counted('Covered parking', coveredParking),
      facing,
      furnishing,
    });
  }, [api, id, about, features, bathrooms, balconies, coveredParking, facing, furnishing]);

  return {
    loading, error, name, about, features,
    bathrooms, balconies, coveredParking, facing, furnishing,
    setAbout, setBathrooms, setBalconies, setCoveredParking, setFacing, setFurnishing,
    toggle: useCallback((f: string) => setFeatures((held) => switched(held, f)), []),
    save,
  };
}
