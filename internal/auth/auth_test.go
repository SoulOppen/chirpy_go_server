package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHashAndCheckPassword(t *testing.T) {
	password := "mi_clave_super_segura"

	// Hashear la contraseña
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	// 1) Verificar que el hash no está vacío
	if hash == "" {
		t.Errorf("Hash está vacío")
	}

	// 2) Verificar que el hash tiene prefijo de bcrypt
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("Hash no tiene prefijo bcrypt esperado: %s", hash)
	}

	// 3) Verificar que la contraseña coincide
	if err := CheckPasswordHash(password, hash); err != nil {
		t.Errorf("CheckPasswordHash() falló con la contraseña correcta: %v", err)
	}

	// 4) Verificar que una contraseña incorrecta falla
	wrongPassword := "contraseña_incorrecta"
	if err := CheckPasswordHash(wrongPassword, hash); err == nil {
		t.Errorf("CheckPasswordHash() debería fallar con contraseña incorrecta")
	}

	// 5) Verificar que generar otro hash con la misma password da un hash distinto (bcrypt usa salt)
	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error al generar segundo hash: %v", err)
	}
	if hash == hash2 {
		t.Errorf("Hash repetido: bcrypt debería generar un hash distinto aunque la contraseña sea igual")
	}
}
func TestMakeAndValidateJWT(t *testing.T) {
	secret := "mySuperSecretKey"
	userID := uuid.New()
	expiration := time.Minute * 5

	token, err := MakeJWT(userID, secret, expiration)
	if err != nil {
		t.Fatalf("MakeJWT error: %v", err)
	}

	returnedID, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("ValidateJWT error: %v", err)
	}

	if returnedID != userID {
		t.Fatalf("Expected userID %v but got %v", userID, returnedID)
	}
}
