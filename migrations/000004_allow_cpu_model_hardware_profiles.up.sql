ALTER TABLE model_hardware_profiles
DROP CONSTRAINT model_hardware_profiles_min_vram_mb_check;

ALTER TABLE model_hardware_profiles
ADD CONSTRAINT model_hardware_profiles_min_vram_mb_check
CHECK (min_vram_mb >= 0);
