import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, Screen, Field, ChoiceRow, Segmented, ErrorState,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { useTeam } from '../src/data/team';

/**
 * Employing a sub-manager (#353).
 *
 * The PAN is typed once and masked the moment it lands (ADR-0013); no Aadhaar
 * number is asked for or held, so it is not on this form.
 */

const employmentTypes = ['Full time', 'Part time', 'Contract', 'Intern'];
const payFrequencies = ['Monthly', 'Fortnightly', 'Weekly', 'Daily'];

const wireType: Record<string, string> = {
  'Full time': 'full_time', 'Part time': 'part_time', Contract: 'contract', Intern: 'intern',
};
const wireFrequency: Record<string, string> = {
  Monthly: 'monthly', Fortnightly: 'fortnightly', Weekly: 'weekly', Daily: 'daily',
};

const pan = /^[A-Z]{5}[0-9]{4}[A-Z]$/;

export default function Employ() {
  const router = useRouter();
  const goBack = useBack('/team');
  const team = useTeam();

  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [email, setEmail] = useState('');
  const [designation, setDesignation] = useState('');
  const [employeeCode, setEmployeeCode] = useState('');
  const [roleID, setRoleID] = useState('');
  const [type, setType] = useState('Full time');
  const [frequency, setFrequency] = useState('Monthly');
  const [joined, setJoined] = useState(new Date().toISOString().slice(0, 10));
  const [pandata, setPAN] = useState('');
  const [salary, setSalary] = useState('');
  const [limit, setLimit] = useState('');
  const [emergencyName, setEmergencyName] = useState('');
  const [emergencyPhone, setEmergencyPhone] = useState('');
  const [refused, setRefused] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const role = team.roles.find((r) => r.id === roleID);

  const submit = async () => {
    const typed = pandata.trim().toUpperCase();
    if (!name.trim()) {
      setRefused('A colleague needs a name.');
      return;
    }
    if (!phone.trim() && !email.trim()) {
      setRefused('A colleague needs a mobile or an email to be reached on.');
      return;
    }
    if (typed && !pan.test(typed)) {
      setRefused('A PAN is ten characters: five letters, four digits, then a letter.');
      return;
    }
    setRefused(null);
    setSaving(true);
    try {
      await team.employ({
        full_name: name.trim(),
        phone: phone.trim(),
        email: email.trim(),
        role_id: roleID,
        designation: designation.trim(),
        employee_code: employeeCode.trim(),
        employment_type: wireType[type],
        pay_frequency: wireFrequency[frequency],
        joined_on: joined,
        pan: typed,
        // Rupees on the form, paise on the wire — money is never a float here.
        salary_minor: Math.round(Number(salary || 0) * 100),
        salary_currency: 'INR',
        emergency_name: emergencyName.trim(),
        emergency_phone: emergencyPhone.trim(),
        property_limit: Number(limit || 0),
        // They hold the record before they sign in, exactly as a reserved owner does.
        state: 'invited',
      });
      router.back();
    } catch (err) {
      setRefused((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <BackHeader title="Add a manager" subtitle="Their terms, and what they look after" onBack={goBack} />
      <Screen>
        {team.error ? <ErrorState error={team.error} onRetry={team.reload} inline /> : null}
        {refused ? <Text style={s.refused} accessibilityRole="alert">{refused}</Text> : null}

        <Card>
          <Text style={s.h}>Who they are</Text>
          <Field label="Full name" value={name} onChange={setName} autoCapitalize="words" />
          <Field label="Mobile" value={phone} onChange={setPhone} keyboardType="phone-pad"
            placeholder="+91…" />
          <Field label="Email" value={email} onChange={setEmail} keyboardType="email-address" />
          <Field label="Designation" value={designation} onChange={setDesignation}
            placeholder="Field executive" />
          <Field label="Employee code" value={employeeCode} onChange={setEmployeeCode}
            autoCapitalize="characters" />
        </Card>

        <Card>
          <Text style={s.h}>What they do</Text>
          <Text style={s.sub}>
            A role sets what a colleague may do and how many buildings they carry, so nothing goes
            unattended.
          </Text>
          <View style={{ marginTop: space(3) }}>
            {team.roles.map((r, i) => (
              <ChoiceRow
                key={r.id}
                label={r.name}
                hint={`Carries up to ${r.property_limit} properties`}
                selected={roleID === r.id}
                onPress={() => setRoleID(r.id)}
                last={i === team.roles.length - 1}
              />
            ))}
          </View>
          {!team.roles.length ? (
            <Text style={s.sub}>No roles defined yet — a colleague can be added without one.</Text>
          ) : null}
          <View style={{ marginTop: space(4) }}>
            <Field
              label="Properties they can carry"
              value={limit}
              onChange={setLimit}
              keyboardType="numeric"
              placeholder={role ? String(role.property_limit) : 'Leave blank for the role’s own limit'}
            />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Their terms</Text>
          <Text style={s.label}>Employment</Text>
          <Segmented items={employmentTypes} value={type} onChange={setType} />
          <View style={{ marginTop: space(4) }}>
            <Field label="Joined on" value={joined} onChange={setJoined} placeholder="YYYY-MM-DD" />
            <Field label="Salary" value={salary} onChange={setSalary} keyboardType="numeric"
              placeholder="In rupees" />
          </View>
          <Text style={s.label}>Paid</Text>
          <Segmented items={payFrequencies} value={frequency} onChange={setFrequency} />
        </Card>

        <Card>
          <Text style={s.h}>For the record</Text>
          <Field label="PAN" value={pandata} onChange={setPAN} autoCapitalize="characters"
            placeholder="ABCDE1234F" />
          <Text style={s.sub}>
            Only the mask is kept. No Aadhaar number is asked for or held.
          </Text>
          <View style={{ marginTop: space(4) }}>
            <Field label="Emergency contact" value={emergencyName} onChange={setEmergencyName}
              autoCapitalize="words" />
            <Field label="Emergency mobile" value={emergencyPhone} onChange={setEmergencyPhone}
              keyboardType="phone-pad" />
          </View>
        </Card>

        <View style={s.actions}>
          <Button label={saving ? 'Adding…' : 'Add to the team'} onPress={submit} style={{ flex: 1 }} />
        </View>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3, lineHeight: 18 },
  label: { ...font.small, color: color.inkSoft, marginBottom: space(2) },
  refused: {
    ...font.small, color: color.negative,
    marginHorizontal: space(4), marginTop: space(4), lineHeight: 18,
  },
  actions: { flexDirection: 'row', marginHorizontal: space(4), marginTop: space(4) },
});
